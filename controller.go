package minke

import (
	"regexp"
	"sync"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listv1beta1 "k8s.io/client-go/listers/extensions/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// Controller is the main thing
type Controller struct {
	clientset  kubernetes.Interface
	namespaces []string
	class      string
	selector   labels.Selector

	ingQueue workqueue.RateLimitingInterface
	ingInf   cache.SharedIndexInformer
	ingLst   listv1beta1.IngressLister

	mutex sync.RWMutex
	ings  map[string][]ingress // Hostnames to ingress mapping
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
	re *regexp.Regexp
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
func New(inff informers.SharedInformerFactory, opts ...Option) (*Controller, error) {
	c := Controller{
		class:      "minke",
		namespaces: []string{""},
		mutex:      sync.RWMutex{},
	}

	for _, opt := range opts {
		if err := opt(&c); err != nil {
			return nil, err
		}
	}

	c.ingQueue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	c.ingInf = inff.Extensions().V1beta1().Ingresses().Informer()
	c.ingLst = inff.Extensions().V1beta1().Ingresses().Lister()

	c.ingInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				c.ingQueue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				c.ingQueue.Add(key)
			}
		},
		UpdateFunc: func(old, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				c.ingQueue.Add(key)
			}
		},
	})

	return &c, nil
}
