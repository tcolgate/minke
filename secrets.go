package minke

// TODO: needs to a way to load CAs instead of full unlocked
// certs (for HTTPS client, and tls peer verification.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog"

	listcorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

type ingressKey struct {
	namespace, name string
}

type secretKey struct {
	namespace, name string
}

type certMapEntry struct {
	sync.RWMutex
	cert *tls.Certificate
	raw  []byte
	sec  secretKey
	ing  ingressKey
}

// a map of host names to certificates.
type certMap struct {
	sync.RWMutex
	set map[string][]*certMapEntry
}

// MarshalJSON lets us report the status of the certificate mapping
func (cm *certMap) MarshalJSON() ([]byte, error) {
	cm.RLock()
	defer cm.RUnlock()
	return json.Marshal(cm.set)
}

func (cme *certMapEntry) MarshalJSON() ([]byte, error) {
	cme.RLock()
	defer cme.RUnlock()
	strmap := map[string]interface{}{
		"ingress":   fmt.Sprintf("%s/%s", cme.ing.namespace, cme.ing.name),
		"secret":    fmt.Sprintf("%s/%s", cme.sec.namespace, cme.sec.name),
		"notBefore": cme.cert.Leaf.NotBefore,
		"notAfter":  cme.cert.Leaf.NotAfter,
		"issuer":    cme.cert.Leaf.Issuer.String(),
		"subject":   cme.cert.Leaf.Subject.String(),
	}
	if len(cme.cert.Leaf.DNSNames) > 0 {
		strmap["dnsName"] = cme.cert.Leaf.DNSNames
	}
	if len(cme.cert.Leaf.IPAddresses) > 0 {
		strmap["ipAddresses"] = cme.cert.Leaf.IPAddresses
	}
	return json.Marshal(strmap)
}

func (cm *certMap) updateIngress(key ingressKey, newset map[string][]*certMapEntry) {
	cm.Lock()
	defer cm.Unlock()
	if cm.set == nil {
		cm.set = make(map[string][]*certMapEntry)
	}
	// filter out the old ones
	for i := range cm.set {
		n := 0
		for _, cme := range cm.set[i] {
			if cme.ing != key {
				cm.set[i][n] = cme
				n++
			}
		}
		cm.set[i] = cm.set[i][:n]
	}

	for h, cmes := range newset {
		cm.set[h] = append(cm.set[h], cmes...)
	}
}

func (cm *certMap) updateSecret(key secretKey, cert *tls.Certificate) {
	cm.RLock()
	defer cm.RUnlock()

	for _, cmes := range cm.set {
		for _, cme := range cmes {
			func() {
				cme.Lock()
				defer cme.Unlock()
				if cme.sec == key {
					cme.cert = cert
				}
			}()
		}
	}
}

// GetCertificate checks for non-expired acceptable matches, and then expired acceptable matches
func (cm *certMap) GetCertificate(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cm.RLock()
	defer cm.RUnlock()
	now := time.Now()

	for _, c := range cm.set[info.ServerName] {
		cert := func() *tls.Certificate {
			c.RLock()
			defer c.RUnlock()
			err := info.SupportsCertificate(c.cert)
			if err != nil {
				return nil
			}
			if c.cert.Leaf.NotBefore.Before(now) &&
				c.cert.Leaf.NotAfter.After(now) {
				return c.cert
			}
			return nil
		}()
		if cert != nil {
			return cert, nil
		}
	}

	name := strings.Split(info.ServerName, ".")
	name[0] = "*"
	wildcardName := strings.Join(name, ".")
	for _, c := range cm.set[wildcardName] {
		if c.cert.Leaf.NotBefore.Before(now) &&
			c.cert.Leaf.NotAfter.After(now) &&
			info.SupportsCertificate(c.cert) != nil {
			return c.cert, nil
		}
	}

	for _, c := range cm.set[info.ServerName] {
		if info.SupportsCertificate(c.cert) != nil {
			return c.cert, nil
		}
	}

	for _, c := range cm.set[wildcardName] {
		if info.SupportsCertificate(c.cert) != nil {
			return c.cert, nil
		}
	}

	return nil, nil
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
	if err != nil {
		log.Printf("keypair error, %v", err)
		return nil
	}
	newcert.Leaf, err = x509.ParseCertificate(newcert.Certificate[0])
	if err != nil {
		log.Printf("parse leaf err, %v", err)
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
	vs, ok := u.secrets[secretKey{namespace, name}]
	u.mu.RUnlock()
	if ok {
		return vs
	}

	sec, err := u.c.secList.Secrets(namespace).Get(name)
	if err != nil {
		return nil
	}

	u.addItem(sec)

	return sec.Data
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
