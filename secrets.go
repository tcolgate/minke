package minke

// TODO: needs to a way to load CAs instead of full unlocked
// certs (for HTTPS client, and tls peer verification.

import (
	"context"
	"crypto/tls"
	"log"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog"

	listcorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

type secretKey struct {
	namespace, name string
}

type secUpdater struct {
	c       *Controller
	mu      sync.RWMutex
	secrets map[secretKey]map[string][]byte

	cacheMu sync.RWMutex
	certs   map[secretKey]*tls.Certificate
}

func (u *secUpdater) updateCert(key secretKey, sec map[string][]byte) *tls.Certificate {
	certBytes := sec[`tls.crt`]
	keyBytes := sec[`tls.key`]
	if len(certBytes) == 0 || len(keyBytes) == 0 {
		// key or cert not specified
		return nil
	}

	newcert, err := tls.X509KeyPair(certBytes, keyBytes)
	log.Printf("got cert %#v", newcert)
	if err != nil {
		return nil
	}
	u.cacheMu.Lock()
	if u.certs == nil {
		u.certs = make(map[secretKey]*tls.Certificate)
	}
	u.certs[key] = &newcert
	u.cacheMu.Unlock()

	return &newcert
}

func (u *secUpdater) getCert(key secretKey) *tls.Certificate {
	u.cacheMu.RLock()
	cert, ok := u.certs[key]
	u.cacheMu.RUnlock()
	if ok {
		return cert
	}

	sec := u.getSecret(key.namespace, key.name)
	log.Printf("got secret %#v", sec)
	return u.updateCert(key, sec)
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

	key := secretKey{sobj.Namespace, sobj.Name}
	u.mu.Lock()
	klog.Infof("secret added, %s/%s", sobj.GetNamespace(), sobj.GetName())
	u.secrets[key] = sobj.Data
	u.mu.Unlock()

	// if the cert is already present, we'll update it
	u.cacheMu.RLock()
	_, ok = u.certs[key]
	u.cacheMu.RUnlock()

	if ok {
		u.updateCert(key, sobj.Data)
	}

	return nil
}

func (u *secUpdater) delItem(obj interface{}) error {
	sobj, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}

	u.cacheMu.Lock()
	defer u.cacheMu.Unlock()
	u.mu.Lock()
	defer u.mu.Unlock()

	klog.Infof("secret deleted, %s/%s", sobj.GetNamespace(), sobj.GetName())
	delete(u.secrets, secretKey{sobj.Namespace, sobj.Name})
	delete(u.certs, secretKey{sobj.Namespace, sobj.Name})
	return nil
}

func (u *secUpdater) getSecret(namespace, name string) map[string][]byte {
	u.mu.RLock()
	defer u.mu.RUnlock()
	vs, ok := u.secrets[secretKey{namespace, name}]
	if !ok {
		log.Printf("no secret found for %v/%v", namespace, name)
	}
	return vs
}

func (c *Controller) setupSecretProcess(ctx context.Context) error {
	upd := &secUpdater{
		c:       c,
		secrets: make(map[secretKey]map[string][]byte),
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
	c.secs = upd

	return nil
}
