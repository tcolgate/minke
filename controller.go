package minke

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	trace "go.opentelemetry.io/otel/trace"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	unversionedcore "k8s.io/client-go/kubernetes/typed/core/v1"

	listcorev1 "k8s.io/client-go/listers/core/v1"
	listextv1beta1 "k8s.io/client-go/listers/extensions/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
)

// Controller is the main thing
type Controller struct {
	client        kubernetes.Interface
	namespaces    []string
	class         string
	selector      labels.Selector
	refresh       time.Duration
	logFunc       func(string, ...interface{})
	accessLogFunc func(string, ...interface{})

	ingProc *processor
	ingList listextv1beta1.IngressLister

	secProc *processor
	secList listcorev1.SecretLister

	epsProc *processor
	epsList listcorev1.EndpointsLister

	recorder  record.EventRecorder
	hasSynced func() bool

	stopLock sync.Mutex
	stopping bool

	transport http.RoundTripper
	http.Handler

	metrics MetricsProvider
	tracer  trace.Tracer

	mutex sync.RWMutex
	ings  ingressSet // Hostnames to ingress mapping
	eps   epsSet     // Service to endpoints mapping
}

// Option for setting controller properties
type Option func(*Controller) error

// WithClass is an option for setting the class
func WithClass(cls string) Option {
	return func(c *Controller) error {
		c.class = cls
		return nil
	}
}

// WithNamespaces is an option for setting the set of namespaces to watch
func WithNamespaces(ns []string) Option {
	return func(c *Controller) error {
		c.namespaces = ns
		return nil
	}
}

// WithSelector is an option for setting a selector to filter the set of
// ingresses we will manage
func WithSelector(s labels.Selector) Option {
	return func(c *Controller) error {
		c.selector = s
		return nil
	}
}

// WithLogFunc is an option for setting a log function
func WithLogFunc(f func(string, ...interface{})) Option {
	return func(c *Controller) error {
		c.logFunc = f
		return nil
	}
}

// WithAccessLogFunc is an option for setting a log function
func WithAccessLogFunc(f func(string, ...interface{})) Option {
	return func(c *Controller) error {
		c.accessLogFunc = f
		return nil
	}
}

// WithTracer is an option for setting a Tracer
func WithTracer(t trace.Tracer) Option {
	return func(c *Controller) error {
		c.tracer = t
		return nil
	}
}

// WithMetricsProvider is an option for setting a provider to
// register and track metrics
func WithMetricsProvider(m MetricsProvider) Option {
	return func(c *Controller) error {
		c.metrics = m
		return nil
	}
}

// New creates a new one
func New(client kubernetes.Interface, opts ...Option) (*Controller, error) {
	c := Controller{
		client:     client,
		class:      "minke",
		namespaces: []string{""},
		mutex:      sync.RWMutex{},
		selector:   labels.Everything(),
		metrics:    metricsProvider,
		tracer:     otel.Tracer("minke"),

		eps: make(map[serviceKey][]*url.URL),
	}

	for _, opt := range opts {
		if err := opt(&c); err != nil {
			return nil, err
		}
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(c.logFunc)
	eventBroadcaster.StartRecordingToSink(&unversionedcore.EventSinkImpl{
		Interface: c.client.CoreV1().Events(""),
	})
	c.recorder = eventBroadcaster.NewRecorder(scheme.Scheme,
		apiv1.EventSource{Component: "loadbalancer-controller"})

	c.setupIngProcess()
	c.setupSecretProcess()
	c.setupEndpointsProcess()
	c.transport = &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	c.Handler = &httputil.ReverseProxy{
		Director:      c.director,
		ErrorLog:      nil,
		FlushInterval: 1 * time.Millisecond,
		Transport:     c.transport,
	}

	if c.metrics != nil {
		c.Handler = c.metrics.NewHTTPServerMetrics(c.Handler)
		c.transport = c.metrics.NewHTTPTransportMetrics(c.transport)
	}

	if c.tracer != nil {
		c.Handler = addInboundTracing(c.tracer, c.Handler)
	}

	return &c, nil
}

func (c *Controller) Run(stopCh <-chan struct{}) {
	go c.ingProc.run(stopCh)
	go c.secProc.run(stopCh)
	go c.epsProc.run(stopCh)

	if !cache.WaitForCacheSync(
		stopCh,
		c.ingProc.hasSynced,
		c.epsProc.hasSynced,
		c.secProc.hasSynced) {
	}

	go c.ingProc.runWorker()
	go c.epsProc.runWorker()
	go c.secProc.runWorker()

	<-stopCh
}

func (c *Controller) Stop() {
	c.stopLock.Lock()
	defer c.stopLock.Unlock()

	if !c.stopping {
		c.ingProc.queue.ShutDown()
		c.secProc.queue.ShutDown()
		c.epsProc.queue.ShutDown()
	}
}

func (c *Controller) HasSynced() bool {
	return (c.ingProc.informer.HasSynced() &&
		c.secProc.informer.HasSynced() &&
		c.epsProc.informer.HasSynced())
}

func (c *Controller) ServeLivezHTTP(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "OK", http.StatusOK)
}

func (c *Controller) ServeReadyzHTTP(w http.ResponseWriter, r *http.Request) {
	if !c.HasSynced() {
		http.Error(w, "Not synced yet", http.StatusInsufficientStorage)
		return
	}
	http.Error(w, "OK", http.StatusOK)
}
