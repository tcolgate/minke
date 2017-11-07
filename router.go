package minke

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"sync"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type epWatcher struct {
	epsQueue workqueue.RateLimitingInterface
	epsInf   cache.SharedIndexInformer
	stop     chan struct{}

	sync.RWMutex
	targets []*url.URL
}

func (c *Controller) newEPInforner(svcKey string) (*epWatcher, error) {
	epw := epWatcher{}
	epw.epsQueue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	epw.epsInf = c.inff.Core().V1().Endpoints().Informer()

	epw.epsInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(old, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				epw.epsQueue.Add(key)
			}
		},
	})
	return &epw, nil
}

func (w *epWatcher) run(ctx context.Context) error {
	stop := make(chan struct{})

	go w.epsInf.Run(stop)
	defer close(stop)

	if !cache.WaitForCacheSync(w.stop, w.epsInf.HasSynced) {
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
			key, quit := w.epsQueue.Get()
			if quit {
				return
			}

			// you always have to indicate to the queue that you've completed a piece of
			// work
			defer w.epsQueue.Done(key)

			err := w.processEPItem(key.(string))

			if err == nil {
				w.epsQueue.Forget(key)
			} else if w.epsQueue.NumRequeues(key) < 4 {
				log.Printf("Error processing %s (will retry): %v", key, err)
				w.epsQueue.AddRateLimited(key)
			} else {
				log.Printf("Error processing %s (giving up): %v", key, err)
				w.epsQueue.Forget(key)
				utilruntime.HandleError(err)
			}
		}()
	}
}

func (w *epWatcher) processEPItem(key string) error {
	log.Printf("Process endpoint key %s", key)

	_, exists, err := w.epsInf.GetIndexer().GetByKey(key)
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
