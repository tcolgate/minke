package minke

import (
	"fmt"
	"log"
	"net"
	"net/url"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	listcorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

type epsUpdater struct {
	c *Controller
}

func (u *epsUpdater) addItem(obj interface{}) error {
	eps, ok := obj.(*corev1.Endpoints)
	if !ok {
		return fmt.Errorf("interface was not an ingress %T", obj)
	}

	u.c.mutex.Lock()
	defer u.c.mutex.Unlock()

	for i := range eps.Subsets {
		set := eps.Subsets[i]
		for j := range set.Ports {
			port := set.Ports[j].Port
			key := ServiceKey{
				namespace: eps.Namespace,
				name:      eps.Name,
				portName:  set.Ports[j].Name,
			}
			var urls []*url.URL
			scheme := "http"
			if port == 443 {
				scheme = "https"
			}
			for k := range set.Addresses {
				urls = append(urls, &url.URL{
					Scheme: scheme,
					Host: net.JoinHostPort(
						set.Addresses[k].IP,
						strconv.Itoa(int(port)),
					),
				})
			}
			u.c.eps[key] = urls
		}
	}

	return nil
}

func (*epsUpdater) delItem(obj interface{}) error {
	eps, ok := obj.(*corev1.Endpoints)
	if !ok {
		return fmt.Errorf("interface was not an ingress %T", obj)
	}

	log.Printf("endpoints deleted, %#v", eps)

	return nil
}

func (c *Controller) setupEndpointsProcess() error {
	upd := &epsUpdater{c}

	c.epsProc = makeProcessor(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.client.CoreV1().Endpoints(metav1.NamespaceAll).List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.client.CoreV1().Endpoints(metav1.NamespaceAll).Watch(options)
			},
		},
		&corev1.Endpoints{},
		c.refresh,
		upd,
	)

	c.epsList = listcorev1.NewEndpointsLister(c.epsProc.informer.GetIndexer())

	return nil
}

func (c *Controller) processEndpointsItem(string) error {
	return nil
}
