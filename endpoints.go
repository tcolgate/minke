package minke

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
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

type epsSet struct {
	set map[serviceKey][]serviceAddr
	sync.RWMutex
}

func backendToServiceKey(namespace string, b *networkingv1beta1.IngressBackend) serviceKey {
	if b.ServicePort.IntVal != 0 {
		return serviceKey{
			namespace: namespace,
			name:      b.ServiceName,
		}
	}
	return serviceKey{
		namespace: namespace,
		name:      b.ServiceName,
		portName:  b.ServicePort.String(),
	}
}

type epsUpdater struct {
	c *Controller
}

func (eps *epsSet) getActiveAddrs(key serviceKey) []serviceAddr {
	eps.RLock()
	defer eps.RUnlock()
	return eps.set[key]
}

func (u *epsUpdater) addItem(obj interface{}) error {
	eps, ok := obj.(*corev1.Endpoints)
	if !ok {
		return fmt.Errorf("interface was not an ingress %T", obj)
	}

	u.clearEndpoints(eps.Namespace, eps.Name)

	portlessKey := serviceKey{
		namespace: eps.Namespace,
		name:      eps.Name,
	}

	for i := range eps.Subsets {
		set := eps.Subsets[i]
		portlessAddrs := make([]serviceAddr, len(set.Addresses))

		for j := range set.Addresses {
			portlessAddrs[j] = serviceAddr{
				addr: set.Addresses[j].IP,
			}
		}
		u.c.eps.Lock()
		u.c.eps.set[portlessKey] = append(u.c.eps.set[portlessKey], portlessAddrs...)
		u.c.eps.Unlock()

		for j := range set.Ports {
			key := serviceKey{
				namespace: eps.Namespace,
				name:      eps.Name,
				portName:  set.Ports[j].Name,
			}

			addrs := make([]serviceAddr, len(set.Addresses))

			port := set.Ports[j].Port

			for j := range set.Addresses {
				addrs[j] = serviceAddr{
					addr: set.Addresses[j].IP,
					port: int(port),
				}
			}
			u.c.eps.Lock()
			u.c.eps.set[key] = append(u.c.eps.set[key], addrs...)
			u.c.eps.Unlock()
		}
	}

	return nil
}

func (u *epsUpdater) clearEndpoints(name, namespace string) {
	u.c.eps.Lock()
	defer u.c.eps.Unlock()
	for key := range u.c.eps.set {
		if key.namespace == namespace &&
			key.name == name {
			delete(u.c.eps.set, key)
		}
	}
}

func (u *epsUpdater) delItem(obj interface{}) error {
	eps, ok := obj.(*corev1.Endpoints)
	if !ok {
		return fmt.Errorf("interface was not an ingress %T", obj)
	}

	u.c.eps.Lock()
	defer u.c.eps.Unlock()
	u.clearEndpoints(eps.Namespace, eps.Name)

	return nil
}

func (c *Controller) setupEndpointsProcess(ctx context.Context) error {
	upd := &epsUpdater{c}

	c.epsProc = makeProcessor(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.client.CoreV1().Endpoints(c.namespace).List(ctx, options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.client.CoreV1().Endpoints(c.namespace).Watch(ctx, options)
			},
		},
		&corev1.Endpoints{},
		c.refresh,
		upd,
	)

	c.epsList = listcorev1.NewEndpointsLister(c.epsProc.informer.GetIndexer())

	return nil
}
