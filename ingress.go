package minke

import (
	"fmt"
	"log"
	"regexp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	extv1beta1 "k8s.io/api/extensions/v1beta1"
	listextv1beta1 "k8s.io/client-go/listers/extensions/v1beta1"

	"k8s.io/client-go/tools/cache"
)

type ingUpdater struct {
	c *Controller
}

func (u *ingUpdater) addItem(obj interface{}) error {
	ing, ok := obj.(*extv1beta1.Ingress)
	if !ok {
		return fmt.Errorf("interface was not an ingress %T", obj)
	}

	newmap := make(map[string][]ingress)

	if !u.c.ourClass(ing) {
		return nil
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
		}
		newmap[ingr.Host] = append(newmap[ingr.Host], ning)
	}

	u.c.mutex.Lock()
	u.c.ings = newmap
	u.c.mutex.Unlock()

	return nil
}

func (*ingUpdater) delItem(obj interface{}) error {
	return nil
}

func (c *Controller) setupIngProcess() error {
	upd := &ingUpdater{c}

	c.ingProc = makeProcessor(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.LabelSelector = c.selector.String()
				return c.client.Extensions().Ingresses(metav1.NamespaceAll).List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = c.selector.String()
				return c.client.Extensions().Ingresses(metav1.NamespaceAll).Watch(options)
			},
		},
		&extv1beta1.Ingress{},
		c.refresh,
		upd,
	)

	c.ingList = listextv1beta1.NewIngressLister(c.ingProc.informer.GetIndexer())

	return nil
}

func (c *Controller) ourClass(ing *extv1beta1.Ingress) bool {
	class, _ := ing.ObjectMeta.Annotations["kubernetes.io/ingress.class"]
	switch {
	// If we have a class set, only match our own.
	case c.class != "" && class != c.class:
		return false
	// If we have no class set, only ingresses with no class.
	case c.class == "" && class != "":
		return false
	default:
		return true
	}
}
