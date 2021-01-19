package minke

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http2"

	"go.opentelemetry.io/otel"
	trace "go.opentelemetry.io/otel/trace"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	unversionedcore "k8s.io/client-go/kubernetes/typed/core/v1"

	listcorev1 "k8s.io/client-go/listers/core/v1"
	listnetworkingv1beta1 "k8s.io/client-go/listers/networking/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
)

// Controller is the main thing
type Controller struct {
	client        kubernetes.Interface
	namespace     string
	class         string
	selector      labels.Selector
	refresh       time.Duration
	logFunc       func(string, ...interface{})
	accessLogFunc func(string, ...interface{})

	defaultBackendNamespace string
	defaultBackendName      string

	defaultTLSSecretNamespace  string
	defaultTLSSecretName       string
	defaultTLSCertificateMutex sync.RWMutex

	clientTLSConfig           *tls.Config
	clientTLSSecretNamespace  string
	clientTLSSecretName       string
	clientTLSCertificate      *tls.Certificate
	clientTLSCertificateMutex sync.RWMutex

	ingProc *processor
	ingList listnetworkingv1beta1.IngressLister

	svcProc *processor
	svcList listcorev1.ServiceLister

	secProc *processor
	secList listcorev1.SecretLister

	epsProc *processor
	epsList listcorev1.EndpointsLister

	recorder  record.EventRecorder
	hasSynced func() bool

	stopLock sync.Mutex
	stopping bool

	transport http.RoundTripper

	proxy *httputil.ReverseProxy
	http.Handler

	metrics MetricsProvider
	tracer  trace.Tracer

	ings *ingressSet // Hostnames to ingress mapping and certs
	svc  *svcUpdater // Service to ports/protocols mapping
	eps  *epsSet     // Service to endpoints mapping
	secs *secUpdater // Secrets

	certMap *certMap
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

// WithNamespace is an option for setting the set of namespaces to watch
func WithNamespace(ns string) Option {
	return func(c *Controller) error {
		c.namespace = ns
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

// WithDefaultBackend sets the backend to be used as defaultBackend when none
// is specified
func WithDefaultBackend(str string) Option {
	return func(c *Controller) error {
		parts := strings.SplitN(str, "/", 2)
		if len(parts) != 2 {
			return fmt.Errorf("default backend service should be in the for of NAMESPACE/NAME")
		}

		c.defaultBackendNamespace = parts[0]
		c.defaultBackendName = parts[1]
		return nil
	}
}

func WithDefaultTLSSecret(str string) Option {
	return func(c *Controller) error {
		if str == "" {
			return nil
		}
		parts := strings.SplitN(str, "/", 2)
		if len(parts) != 2 {
			return fmt.Errorf("default TLS secret should be in the for of NAMESPACE/NAME")
		}

		c.defaultTLSSecretNamespace = parts[0]
		c.defaultTLSSecretName = parts[1]
		return nil
	}
}

func WithClientTLSSecret(str string) Option {
	return func(c *Controller) error {
		if str == "" {
			return nil
		}
		parts := strings.SplitN(str, "/", 2)
		if len(parts) != 2 {
			return fmt.Errorf("default client TLS secret should be in the for of NAMESPACE/NAME")
		}

		c.clientTLSSecretNamespace = parts[0]
		c.clientTLSSecretName = parts[1]
		return nil
	}
}

func WithClientTLSConfig(cfg *tls.Config) Option {
	return func(c *Controller) error {
		if cfg == nil {
			return nil
		}
		c.clientTLSConfig = cfg.Clone()
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
		client:          client,
		class:           "minke",
		namespace:       metav1.NamespaceAll,
		selector:        labels.Everything(),
		metrics:         metricsProvider,
		tracer:          otel.Tracer("minke"),
		clientTLSConfig: &tls.Config{},
		certMap:         &certMap{},
		ings:            &ingressSet{},
		eps:             &epsSet{set: make(map[serviceKey][]serviceAddr)},
	}

	for _, opt := range opts {
		if err := opt(&c); err != nil {
			return nil, err
		}
	}

	// TODO(): verify that any default secrets are in the same namespace as any
	// namespace, or the watch wont see them. (or stop using listwatch for secrets)

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(c.logFunc)
	eventBroadcaster.StartRecordingToSink(&unversionedcore.EventSinkImpl{
		Interface: c.client.CoreV1().Events(""),
	})
	c.recorder = eventBroadcaster.NewRecorder(scheme.Scheme,
		apiv1.EventSource{Component: "loadbalancer-controller"})

	ctx := context.Background()
	c.setupIngProcess(ctx)
	c.setupServiceProcess(ctx)
	c.setupSecretProcess(ctx)
	c.setupEndpointsProcess(ctx)

	c.clientTLSConfig.GetClientCertificate = c.GetClientCertificate

	transport1 := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 10 * time.Second,
		TLSClientConfig:       c.clientTLSConfig,
	}

	transport2 := &http2.Transport{
		AllowHTTP: true,
		DialTLS: func(netw, addr string, cfg *tls.Config) (net.Conn, error) {
			return net.Dial(netw, addr)
		},
	}

	c.transport = &httpTransport{
		base:  transport1,
		http2: transport2,
	}

	c.proxy = &httputil.ReverseProxy{
		Director:      c.director,
		FlushInterval: 10 * time.Millisecond,
		Transport:     c.transport,
		ErrorHandler:  c.errorHandler,
	}

	c.Handler = http.HandlerFunc(c.handler)

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
	go c.svcProc.run(stopCh)
	go c.epsProc.run(stopCh)

	if !cache.WaitForCacheSync(
		stopCh,
		c.ingProc.hasSynced,
		c.svcProc.hasSynced,
		c.epsProc.hasSynced,
		c.secProc.hasSynced) {
	}

	go c.ingProc.runWorker()
	go c.svcProc.runWorker()
	go c.epsProc.runWorker()
	go c.secProc.runWorker()

	<-stopCh
}

func (c *Controller) Stop() {
	c.stopLock.Lock()
	defer c.stopLock.Unlock()

	if !c.stopping {
		c.ingProc.queue.ShutDown()
		c.svcProc.queue.ShutDown()
		c.secProc.queue.ShutDown()
		c.epsProc.queue.ShutDown()
	}
}

func (c *Controller) HasSynced() bool {
	return (c.ingProc.informer.HasSynced() &&
		c.svcProc.informer.HasSynced() &&
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

// MarshalJSON lets us report the status of the controller
func (c *Controller) MarshalJSON() ([]byte, error) {
	status := map[string]interface{}{
		"ingresses": c.ings,
		"certs":     c.certMap,
		"endpoints": c.eps,
	}
	return json.Marshal(status)
}

func (c *Controller) ServeStatusHTTP(w http.ResponseWriter, r *http.Request) {
	bs, err := json.Marshal(c)
	if err != nil {
		klog.Errorf(`error serving status, %v`, err)
		http.Error(w, `{"error": "status failed, see logs"}`, http.StatusOK)
		return
	}
	io.Copy(w, bytes.NewBuffer(bs))
}

func (c *Controller) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	klog.Infof("proxy: %#v", err)
	w.WriteHeader(http.StatusBadGateway)
}
