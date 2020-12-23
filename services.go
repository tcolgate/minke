package minke

import (
	"context"
	"encoding/json"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog"

	listcorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

type svcKey struct {
	namespace, name string
}

type svcItem struct {
	svc       *corev1.Service
	appProtos map[string]string
}

type svcUpdater struct {
	c    *Controller
	mu   sync.RWMutex
	svcs map[svcKey]svcItem
}

var annAppProtos = "service.alpha.kubernetes.io/app-protocol"

func (u *svcUpdater) addItem(obj interface{}) error {
	sobj, ok := obj.(*corev1.Service)
	if !ok {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()

	var appProtos map[string]string

	appsJSON := sobj.Annotations[annAppProtos]
	json.Unmarshal([]byte(appsJSON), &appProtos)

	for _, p := range sobj.Spec.Ports {
		if p.AppProtocol != nil {
			appProtos[p.Name] = *p.AppProtocol
		}
	}

	u.svcs[svcKey{sobj.Namespace, sobj.Name}] = svcItem{
		svc:       sobj,
		appProtos: appProtos,
	}
	return nil
}

func (u *svcUpdater) delItem(obj interface{}) error {
	sobj, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	klog.Infof("secret deleted, %s/%s", sobj.GetNamespace(), sobj.GetName())
	delete(u.svcs, svcKey{sobj.Namespace, sobj.Name})
	return nil
}

func (u *svcUpdater) getServicePortScheme(key serviceKey) string {
	u.mu.RLock()
	defer u.mu.RUnlock()
	v, ok := u.svcs[svcKey{key.namespace, key.name}]
	if !ok || v.appProtos == nil {
		return "http"
	}

	if proto, ok := v.appProtos[key.portName]; ok {
		switch proto {
		case "HTTP":
			return "http"
		case "HTTP2":
			return "http2"
		case "HTTPS":
			return "https"
		default:
			return "http"
		}
	}
	return "http"
}

func (c *Controller) setupServiceProcess(ctx context.Context) error {
	upd := &svcUpdater{
		c:    c,
		svcs: make(map[svcKey]svcItem),
	}

	c.svcProc = makeProcessor(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.client.CoreV1().Services(metav1.NamespaceAll).List(ctx, options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.client.CoreV1().Services(metav1.NamespaceAll).Watch(ctx, options)
			},
		},
		&corev1.Service{},
		c.refresh,
		upd,
	)

	c.svcList = listcorev1.NewServiceLister(c.svcProc.informer.GetIndexer())
	c.svc = upd

	return nil
}

func (c *Controller) processSvcItem(string) error {
	return nil
}
