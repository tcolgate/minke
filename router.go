package minke

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type epWatcher struct {
	queue workqueue.RateLimitingInterface
	inf   cache.SharedIndexInformer
	stop  chan struct{}

	sync.RWMutex
	targets []*url.URL
}

func (c *Controller) newEPInforner(namespace, name string) (*epWatcher, error) {
	w := epWatcher{}
	w.queue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	w.inf = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.LabelSelector = "name=" + name
				return c.client.CoreV1().Endpoints(namespace).List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = "name=" + name
				return c.client.CoreV1().Endpoints(metav1.NamespaceAll).Watch(options)
			},
		},
		&corev1.Endpoints{},
		c.refresh,
		cache.Indexers{},
	)

	w.inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(old, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				w.queue.Add(key)
			}
		},
	})
	return &w, nil
}

func (w *epWatcher) run(ctx context.Context) error {
	stop := make(chan struct{})

	go w.inf.Run(stop)
	defer close(stop)

	if !cache.WaitForCacheSync(w.stop, w.inf.HasSynced) {
		log.Print("Timed out waiting for eps caches to sync")
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-w.stop:
			return nil
		default:
		}

		func() {
			key, quit := w.queue.Get()
			if quit {
				return
			}

			// you always have to indicate to the queue that you've completed a piece of
			// work
			defer w.queue.Done(key)

			err := w.processEPItem(key.(string))

			if err == nil {
				w.queue.Forget(key)
			} else if w.queue.NumRequeues(key) < 4 {
				log.Printf("Error processing %s (will retry): %v", key, err)
				w.queue.AddRateLimited(key)
			} else {
				log.Printf("Error processing %s (giving up): %v", key, err)
				w.queue.Forget(key)
				utilruntime.HandleError(err)
			}
		}()
	}
}

func (w *epWatcher) processEPItem(key string) error {
	log.Printf("Process endpoint key %s", key)

	_, exists, err := w.inf.GetIndexer().GetByKey(key)
	if err != nil {
		return fmt.Errorf("Error fetching object with key %s from store: %v", key, err)
	}

	if !exists {
		log.Printf("Process delete endpoint key %s", key)
		return nil
	}

	log.Printf("Process update endpoint key %s", key)
	return nil
}

/*
	c.epsQueue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	c.epsInf = inff.Core().V1().Endpoints().Informer()
	c.epsInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				c.epsQueue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				c.epsQueue.Add(key)
			}
		},
	})

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
				key, quit := c.epsQueue.Get()
				if quit {
					return
				}

				// you always have to indicate to the queue that you've completed a piece of
				// work
				defer c.epsQueue.Done(key)

				err := c.processEPItem(key.(string))

				if err == nil {
					c.epsQueue.Forget(key)
				} else if c.epsQueue.NumRequeues(key) < 4 {
					log.Printf("Error processing %s (will retry): %v", key, err)
					c.epsQueue.AddRateLimited(key)
				} else {
					log.Printf("Error processing %s (giving up): %v", key, err)
					c.epsQueue.Forget(key)
					utilruntime.HandleError(err)
				}
			}()
		}
	}()
*/

type secretWatcher struct {
	queue workqueue.RateLimitingInterface
	inf   cache.SharedIndexInformer
	stop  chan struct{}

	sync.RWMutex
}

func (c *Controller) newSecretInforner(namespace, name string) (*secretWatcher, error) {
	w := secretWatcher{}
	w.queue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	w.inf = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.LabelSelector = "name=" + name
				return c.client.CoreV1().Secrets(namespace).List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = "name=" + name
				return c.client.CoreV1().Secrets(namespace).Watch(options)
			},
		},
		&corev1.Secret{},
		c.refresh,
		cache.Indexers{},
	)

	w.inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(old, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				w.queue.Add(key)
			}
		},
	})
	return &w, nil
}

func (w *secretWatcher) run(ctx context.Context) error {
	stop := make(chan struct{})

	go w.inf.Run(stop)
	defer close(stop)

	if !cache.WaitForCacheSync(w.stop, w.inf.HasSynced) {
		log.Print("Timed out waiting for eps caches to sync")
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-w.stop:
			return nil
		default:
		}

		func() {
			key, quit := w.queue.Get()
			if quit {
				return
			}

			// you always have to indicate to the queue that you've completed a piece of
			// work
			defer w.queue.Done(key)

			err := w.processSecretItem(key.(string))

			if err == nil {
				w.queue.Forget(key)
			} else if w.queue.NumRequeues(key) < 4 {
				log.Printf("Error processing %s (will retry): %v", key, err)
				w.queue.AddRateLimited(key)
			} else {
				log.Printf("Error processing %s (giving up): %v", key, err)
				w.queue.Forget(key)
				utilruntime.HandleError(err)
			}
		}()
	}
}

func (w *secretWatcher) processSecretItem(key string) error {
	log.Printf("Process secrets key %s", key)

	_, exists, err := w.inf.GetIndexer().GetByKey(key)
	if err != nil {
		return fmt.Errorf("Error fetching object with key %s from store: %v", key, err)
	}

	if !exists {
		log.Printf("Process delete secrets key %s", key)
		return nil
	}

	log.Printf("Process update secrets key %s", key)
	return nil
}
