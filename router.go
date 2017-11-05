package minke

import (
	"fmt"
	"log"
	"net/url"
	"sync"

	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type epWatcher struct {
	epsQueue workqueue.RateLimitingInterface
	epsInf   cache.SharedIndexInformer

	sync.RWMutex
	targets []*url.URL
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
