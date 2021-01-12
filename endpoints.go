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

	portlessKey := serviceKey{
		namespace: eps.Namespace,
		name:      eps.Name,
	}

	addrs := make(map[serviceKey][]serviceAddr)

	for i := range eps.Subsets {
		set := eps.Subsets[i]

		for j := range set.Addresses {
			addrs[portlessKey] = append(addrs[portlessKey], serviceAddr{
				addr: set.Addresses[j].IP,
			})
		}

		for j := range set.Ports {
			key := serviceKey{
				namespace: eps.Namespace,
				name:      eps.Name,
				portName:  set.Ports[j].Name,
			}

			port := set.Ports[j].Port

			for j := range set.Addresses {
				addrs[key] = append(addrs[key], serviceAddr{
					addr: set.Addresses[j].IP,
					port: int(port),
				})
			}
		}
	}

	u.c.eps.Lock()
	for k, v := range addrs {
		u.c.eps.set[k] = v
	}
	u.c.eps.Unlock()

	return nil
}

func (u *epsUpdater) clearEndpoints(name, namespace string) {
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
