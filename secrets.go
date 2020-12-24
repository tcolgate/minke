package minke

import (
	"context"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog"

	listcorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

type secKey struct {
	namespace, name string
}

type secUpdater struct {
	c       *Controller
	mu      sync.RWMutex
	secrets map[secKey]map[string][]byte
}

func (u *secUpdater) addItem(obj interface{}) error {
	sobj, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}

	if sobj.Data == nil {
		return nil
	}

	_, hasCert := sobj.Data["tls.crt"]
	_, hasKey := sobj.Data["tls.key"]
	if !hasCert || !hasKey {
		return nil
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	klog.Infof("secret added, %s/%s", sobj.GetNamespace(), sobj.GetName())
	u.secrets[secKey{sobj.Namespace, sobj.Name}] = sobj.Data
	return nil
}

func (u *secUpdater) delItem(obj interface{}) error {
	sobj, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	klog.Infof("secret deleted, %s/%s", sobj.GetNamespace(), sobj.GetName())
	delete(u.secrets, secKey{sobj.Namespace, sobj.Name})
	return nil
}

func (u *secUpdater) getSecret(namespace, name string) map[string][]byte {
	u.mu.RLock()
	defer u.mu.RUnlock()
	vs, _ := u.secrets[secKey{namespace, name}]
	return vs
}

func (c *Controller) setupSecretProcess(ctx context.Context) error {
	upd := &secUpdater{
		c:       c,
		secrets: make(map[secKey]map[string][]byte),
	}

	c.secProc = makeProcessor(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.client.CoreV1().Secrets(c.namespace).List(ctx, options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.client.CoreV1().Secrets(c.namespace).Watch(ctx, options)
			},
		},
		&corev1.Secret{},
		c.refresh,
		upd,
	)

	c.secList = listcorev1.NewSecretLister(c.secProc.informer.GetIndexer())

	return nil
}

func (c *Controller) processSecItem(string) error {
	return nil
}
