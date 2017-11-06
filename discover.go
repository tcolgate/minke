package minke

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sync"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	extv1beta1 "k8s.io/api/extensions/v1beta1"
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

// New creates a new one
func New(inff informers.SharedInformerFactory, selector labels.Selector) *Controller {
	c := Controller{
		mutex:    sync.RWMutex{},
		selector: selector,
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

	return &c
}

// Run just  harness
func (c *Controller) Run(ctx context.Context) {
	defer utilruntime.HandleCrash()

	stop := make(chan struct{})

	go func() {
		<-ctx.Done()
		close(stop)
	}()

	go c.ingInf.Run(stop)

	if !cache.WaitForCacheSync(stop, c.ingInf.HasSynced) {
		log.Print("Timed out waiting for caches to sync")
	}

	wg := &sync.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			default:
			}

			func() {
				key, quit := c.ingQueue.Get()
				if quit {
					return
				}

				// you always have to indicate to the queue that you've completed a piece of
				// work
				defer c.ingQueue.Done(key)

				err := c.processIngressItem(key.(string))

				if err == nil {
					c.ingQueue.Forget(key)
				} else if c.ingQueue.NumRequeues(key) < 4 {
					log.Printf("Error processing %s (will retry): %v", key, err)
					c.ingQueue.AddRateLimited(key)
				} else {
					log.Printf("Error processing %s (giving up): %v", key, err)
					c.ingQueue.Forget(key)
					utilruntime.HandleError(err)
				}
			}()
		}
	}()

	wg.Wait()
}

func (c *Controller) processIngressItem(key string) error {
	log.Printf("Process ingress key %s", key)

	_, exists, err := c.ingInf.GetIndexer().GetByKey(key)
	if err != nil {
		return fmt.Errorf("Error fetching object with key %s from store: %v", key, err)
	}

	if !exists {
		log.Printf("Process delete ingress key %s", key)
		return nil
	}

	log.Printf("Process update ingress key %s", key)
	return nil
}

func (c *Controller) updateIngresses(key string) error {
	var ings []*extv1beta1.Ingress
	for _, n := range c.namespaces {
		nings, err := c.ingLst.Ingresses(n).List(c.selector)
		if err != nil {
			continue
		}
		ings = append(ings, nings...)
	}

	newmap := make(map[string][]ingress)

	for _, ing := range ings {
		for _, ingr := range ing.Spec.Rules {
			ning := ingress{
				defaultBackend: backend{
					svc:     ing.Spec.Backend.ServiceName,
					svcPort: ing.Spec.Backend.ServicePort,
				},
			}
			for _, ingp := range ingr.HTTP.Paths {
				re, err := regexp.CompilePOSIX(ingp.Path)
				if err != nil {
					continue
				}
				nir := ingressRule{
					re: re,
					backend: backend{
						svc:     ingp.Backend.ServiceName,
						svcPort: ingp.Backend.ServicePort,
					},
				}
				ning.rules = append(ning.rules, nir)
			}
			newmap[ingr.Host] = append(newmap[ingr.Host], ning)
		}
	}

	c.mutex.Lock()
	// Stop listers from the old set
	//oldings := c.ings
	c.ings = newmap
	c.mutex.Unlock()

	return nil
}
