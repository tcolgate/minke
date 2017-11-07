package minke

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sync"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	extv1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/client-go/tools/cache"
)

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

				err := c.processIngressItem(ctx, key.(string))

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

func (c *Controller) processIngressItem(ctx context.Context, key string) error {
	log.Printf("Process ingress key %s", key)

	_, exists, err := c.ingInf.GetIndexer().GetByKey(key)
	if err != nil {
		return fmt.Errorf("Error fetching object with key %s from store: %v", key, err)
	}

	if !exists {
		log.Printf("Process delete ingress key %s", key)
		c.updateIngresses(ctx, key)
		return nil
	}

	log.Printf("Process update ingress key %s", key)
	c.updateIngresses(ctx, key)
	return nil
}

func (c *Controller) updateIngresses(ctx context.Context, key string) error {
	var ings []*extv1beta1.Ingress
	for _, n := range c.namespaces {
		nings, err := c.ingLst.Ingresses(n).List(c.selector)
		if err != nil {
			continue
		}
		ings = append(ings, nings...)
	}

	newmap := make(map[string][]ingress)
	newepmap := make(map[string]*epWatcher)

	for _, ing := range ings {
		class, _ := ing.ObjectMeta.Annotations["kubernetes.io/ingress.class"]
		switch {
		// If we have a class set, only match our own.
		case c.class != "" && class != c.class:
			continue
		// If we have no class set, only ingresses with no class.
		case c.class == "" && class != "":
			continue
		default:
		}

		for _, ingr := range ing.Spec.Rules {
			log.Printf("ING: %#v", ingr)
			ning := ingress{}
			if ing.Spec.Backend != nil {
				ning.defaultBackend = backend{
					svc:     ing.Spec.Backend.ServiceName,
					svcPort: ing.Spec.Backend.ServicePort,
				}
			}
			for _, ingp := range ingr.HTTP.Paths {
				path := "^/.+"
				if ingp.Path != "" {
					if ingp.Path[0] != '/' {
						// TODO: log an error
						continue
					}
					path = "^" + ingp.Path
				}
				re, err := regexp.CompilePOSIX(path)
				if err != nil {
					// TODO: log an error
					continue
				}
				nir := ingressRule{
					host: ingr.Host,
					re:   re,
					backend: backend{
						svc:     ingp.Backend.ServiceName,
						svcPort: ingp.Backend.ServicePort,
					},
				}
				ning.rules = append(ning.rules, nir)
				svcKey := fmt.Sprintf("%s/%s", ing.ObjectMeta.Namespace, ingp.Backend.ServiceName)
				if _, ok := newepmap[svcKey]; !ok {
					svcInf, err := c.newEPInforner(svcKey)
					if err != nil {
						// TODO: log error
						continue
					}
					newepmap[svcKey] = svcInf
				}
			}
			newmap[ingr.Host] = append(newmap[ingr.Host], ning)
		}
	}

	for _, epw := range newepmap {
		go epw.run(ctx)
	}

	c.mutex.Lock()
	// Stop listers from the old set
	//oldings := c.ings
	oldepmap := c.epWatchers
	c.ings = newmap
	// update the endpoint informers
	c.epWatchers = newepmap
	c.mutex.Unlock()

	// stop old endpoint watchers.
	for _, oep := range oldepmap {
		log.Printf("OEP: %#v", oep)
		close(oep.stop)
	}

	return nil
}
