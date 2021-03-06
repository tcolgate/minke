package minke

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

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

func (sk serviceKey) String() string {
	if sk.portName == "" {
		return fmt.Sprintf("%s/%s", sk.namespace, sk.name)
	}
	return fmt.Sprintf("%s/%s:%s", sk.namespace, sk.name, sk.portName)
}

func (sk serviceKey) MarshalJSON() ([]byte, error) {
	return json.Marshal(sk.String())
}

type serviceAddrSet struct {
	addrs []serviceAddr
	index uint64
}

type serviceAddr struct {
	addr string
	port int
}

func (sa serviceAddr) String() string {
	if sa.port != 0 {
		return fmt.Sprintf("%s:%v", sa.addr, sa.port)
	}
	return fmt.Sprintf("%s", sa.addr)
}

func (sa serviceAddr) MarshalJSON() ([]byte, error) {
	return json.Marshal(sa.String())
}

type epsSet struct {
	set map[serviceKey]*serviceAddrSet
	sync.RWMutex
}

// MarshalJSON lets us report the status of the certificate mapping
func (eps *epsSet) MarshalJSON() ([]byte, error) {
	eps.RLock()
	defer eps.RUnlock()
	strmap := map[string][]string{}
	for k, vs := range eps.set {
		kstr := k.String()
		for _, v := range vs.addrs {
			strmap[kstr] = append(strmap[kstr], v.String())
		}
	}
	return json.Marshal(strmap)
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

func (eps *epsSet) getNextAddr(key serviceKey) serviceAddr {
	eps.RLock()
	set := eps.set[key]
	eps.RUnlock()
	if set == nil {
		return serviceAddr{}
	}
	count := atomic.AddUint64(&set.index, 1)
	addr := set.addrs[count%uint64(len(set.addrs))]
	return addr
}

func (eps *epsSet) getActiveAddrs(key serviceKey) []serviceAddr {
	eps.RLock()
	defer eps.RUnlock()

	return eps.set[key].addrs
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

	addrsset := make(map[serviceKey]*serviceAddrSet)

	for i := range eps.Subsets {
		set := eps.Subsets[i]

		for j := range set.Addresses {
			if addrsset[portlessKey] == nil {
				addrsset[portlessKey] = &serviceAddrSet{}
			}
			addrsset[portlessKey].addrs = append(addrsset[portlessKey].addrs, serviceAddr{
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
				if addrsset[key] == nil {
					addrsset[key] = &serviceAddrSet{}
				}
				addrsset[key].addrs = append(addrsset[key].addrs, serviceAddr{
					addr: set.Addresses[j].IP,
					port: int(port),
				})
			}
		}
	}

	u.c.eps.Lock()
	if u.c.eps.set == nil {
		u.c.eps.set = make(map[serviceKey]*serviceAddrSet)
	}

	for k, v := range addrsset {
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
