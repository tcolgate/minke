package minke

import (
	"regexp"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"

	extv1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/client-go/kubernetes"
	listv1beta1 "k8s.io/client-go/listers/extensions/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// Controller is the main thing
type Controller struct {
	client     kubernetes.Interface
	namespaces []string
	class      string
	selector   labels.Selector
	refresh    time.Duration

	queue workqueue.RateLimitingInterface
	inf   cache.SharedIndexInformer
	lst   listv1beta1.IngressLister

	mutex sync.RWMutex
	ings  map[string][]ingress // Hostnames to ingress mapping

	epWatchers     map[string]*epWatcher
	secretWatchers map[string]*secretWatcher
}

type backend struct {
	svc     string
	svcPort intstr.IntOrString
}

type ingress struct {
	defaultBackend backend
	rules          []ingressRule
}

type ingressRule struct {
	host string
	re   *regexp.Regexp
	backend
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

// New creates a new one
func New(client kubernetes.Interface, opts ...Option) (*Controller, error) {
	c := Controller{
		client:     client,
		class:      "minke",
		namespaces: []string{""},
		mutex:      sync.RWMutex{},
		selector:   labels.Everything(),
	}

	for _, opt := range opts {
		if err := opt(&c); err != nil {
			return nil, err
		}
	}

	c.queue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	c.inf = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.LabelSelector = c.selector.String()
				return client.Extensions().Ingresses(metav1.NamespaceAll).List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = c.selector.String()
				return client.Extensions().Ingresses(metav1.NamespaceAll).Watch(options)
			},
		},
		&extv1beta1.Ingress{},
		c.refresh,
		cache.Indexers{},
	)

	c.inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				c.queue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				c.queue.Add(key)
			}
		},
		UpdateFunc: func(old, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				c.queue.Add(key)
			}
		},
	})

	return &c, nil
}
