package minke

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	extv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	listcorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

type serviceKey struct {
	namespace string
	name      string
	portName  string
}

type serviceAddr struct {
	addr string
	port int
}

type epsSet map[serviceKey][]serviceAddr

func backendToServiceKey(namespace string, b *extv1beta1.IngressBackend) serviceKey {
	res := serviceKey{
		namespace: namespace,
		name:      b.ServiceName,
		portName:  b.ServicePort.StrVal,
	}
	return res
}

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
	u.clearEndpoints(eps.Namespace, eps.Name)

	for i := range eps.Subsets {
		set := eps.Subsets[i]
		for j := range set.Ports {
			port := set.Ports[j].Port
			key := serviceKey{
				namespace: eps.Namespace,
				name:      eps.Name,
				portName:  set.Ports[j].Name,
			}
			var addrs []serviceAddr
			for k := range set.Addresses {
				addrs = append(addrs, serviceAddr{
					addr: set.Addresses[k].IP,
					port: int(port),
				})
			}
			u.c.eps[key] = addrs
		}
	}

	return nil
}

func (u *epsUpdater) clearEndpoints(name, namespace string) {
	for key := range u.c.eps {
		if key.namespace == namespace &&
			key.name == name {
			delete(u.c.eps, key)
		}
	}
}

func (u *epsUpdater) delItem(obj interface{}) error {
	eps, ok := obj.(*corev1.Endpoints)
	if !ok {
		return fmt.Errorf("interface was not an ingress %T", obj)
	}

	u.c.mutex.Lock()
	defer u.c.mutex.Unlock()
	u.clearEndpoints(eps.Namespace, eps.Name)

	return nil
}

func (c *Controller) setupEndpointsProcess(ctx context.Context) error {
	upd := &epsUpdater{c}

	c.epsProc = makeProcessor(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.client.CoreV1().Endpoints(metav1.NamespaceAll).List(ctx, options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.client.CoreV1().Endpoints(metav1.NamespaceAll).Watch(ctx, options)
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
