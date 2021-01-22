package minke

// TODO: needs to a way to load CAs instead of full unlocked
// certs (for HTTPS client, and tls peer verification.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

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
	err  error
}

// a map of host names to certificates.
type certMap struct {
	sync.RWMutex
	secs     *secUpdater
	set      map[string][]*certMapEntry
	defaults []*certMapEntry
}

// MarshalJSON lets us report the status of the certificate mapping
func (cm *certMap) MarshalJSON() ([]byte, error) {
	cm.RLock()
	defer cm.RUnlock()
	strmap := map[string]interface{}{
		"ingressCerts": cm.set,
		"defaultCerts": cm.defaults,
	}
	return json.Marshal(strmap)
}

func (cme *certMapEntry) MarshalJSON() ([]byte, error) {
	cme.RLock()
	defer cme.RUnlock()
	strmap := map[string]interface{}{
		"ingress": fmt.Sprintf("%s/%s", cme.ing.namespace, cme.ing.name),
		"secret":  fmt.Sprintf("%s/%s", cme.sec.namespace, cme.sec.name),
	}

	if cme.cert != nil {
		strmap["notBefore"] = cme.cert.Leaf.NotBefore
		strmap["notAfter"] = cme.cert.Leaf.NotAfter
		strmap["issuer"] = cme.cert.Leaf.Issuer.String()
		strmap["subject"] = cme.cert.Leaf.Subject.String()
		if len(cme.cert.Leaf.DNSNames) > 0 {
			strmap["dnsName"] = cme.cert.Leaf.DNSNames
		}
		if len(cme.cert.Leaf.IPAddresses) > 0 {
			strmap["ipAddresses"] = cme.cert.Leaf.IPAddresses
		}
	}
	if cme.err != nil {
		strmap["error"] = cme.err
	}

	return json.Marshal(strmap)
}

func (cm *certMap) updateIngress(key ingressKey, sec secretKey, hosts []string) {
	cert, err := cm.secs.getCert(sec)
	cmapEntry := &certMapEntry{
		sec:  sec,
		ing:  key,
		cert: cert,
		err:  err,
	}
	newset := make(map[string][]*certMapEntry, len(hosts))
	for _, h := range hosts {
		newset[h] = [](*certMapEntry){cmapEntry}
	}

	cm.Lock()
	defer cm.Unlock()
	if cm.set == nil {
		cm.set = make(map[string][]*certMapEntry)
	}
	// filter out the old ones
	for h := range cm.set {
		n := 0
		for _, cme := range cm.set[h] {
			if cme.ing != key {
				cm.set[h][n] = cme
				n++
			}
		}
		cm.set[h] = cm.set[h][:n]
	}

	for h, cmes := range newset {
		cm.set[h] = append(cm.set[h], cmes...)
	}
}

func (cm *certMap) updateSecret(key secretKey, cert *tls.Certificate, err error) {
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

	for _, cme := range cm.defaults {
		func() {
			cme.Lock()
			defer cme.Unlock()
			if cme.sec == key {
				cme.cert = cert
			}
		}()
	}
}

func (cm *certMap) setDefaults(keys []secretKey) {
	var certs []*certMapEntry
	for _, key := range keys {
		cert, err := cm.secs.getCert(key)
		certs = append(certs, &certMapEntry{
			sec:  key,
			cert: cert,
			err:  err,
		})
	}

	cm.Lock()
	defer cm.Unlock()
	cm.defaults = certs
}

// GetCertificate checks for non-expired acceptable matches, and then expired acceptable matches
// and then hail-mary the first default certs, if we have any.
func (cm *certMap) GetCertificate(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cm.RLock()
	defer cm.RUnlock()
	now := time.Now()

	for _, c := range cm.set[info.ServerName] {
		c.RLock()
		cert := c.cert
		c.RUnlock()
		if cert == nil {
			continue
		}

		if cert.Leaf.NotBefore.Before(now) &&
			cert.Leaf.NotAfter.After(now) &&
			info.SupportsCertificate(cert) == nil {
			return cert, nil
		}
	}

	name := strings.Split(info.ServerName, ".")
	name[0] = "*"
	wildcardName := strings.Join(name, ".")
	for _, c := range cm.set[wildcardName] {
		c.RLock()
		cert := c.cert
		c.RUnlock()
		if cert == nil {
			continue
		}

		if cert.Leaf.NotBefore.Before(now) &&
			cert.Leaf.NotAfter.After(now) &&
			info.SupportsCertificate(c.cert) == nil {
			return cert, nil
		}
	}

	for _, c := range cm.defaults {
		c.RLock()
		cert := c.cert
		c.RUnlock()
		if cert == nil {
			continue
		}

		if cert.Leaf.NotBefore.Before(now) &&
			cert.Leaf.NotAfter.After(now) &&
			info.SupportsCertificate(cert) == nil {
			return cert, nil
		}
	}

	for _, c := range cm.set[info.ServerName] {
		c.RLock()
		cert := c.cert
		c.RUnlock()
		if cert == nil {
			continue
		}

		if info.SupportsCertificate(cert) == nil {
			return cert, nil
		}
	}

	for _, c := range cm.set[wildcardName] {
		c.RLock()
		cert := c.cert
		c.RUnlock()
		if cert == nil {
			continue
		}

		if info.SupportsCertificate(cert) == nil {
			return cert, nil
		}
	}

	var lastOption *tls.Certificate
	for _, c := range cm.defaults {
		c.RLock()
		cert := c.cert
		c.RUnlock()
		if cert == nil {
			continue
		}

		if lastOption == nil {
			lastOption = cert
		}
		if info.SupportsCertificate(cert) == nil {
			return cert, nil
		}
	}

	return lastOption, nil
}

type secUpdater struct {
	c       *Controller
	mu      sync.RWMutex
	secrets map[secretKey]map[string][]byte
	certMap *certMap
}

func (u *secUpdater) updateCert(key secretKey) {
	sec, err := u.getCert(key)
	u.certMap.updateSecret(key, sec, err)
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
	klog.Infof("secret %s/%s updated", sobj.GetNamespace(), sobj.GetName())
	u.secrets[key] = sobj.Data
	u.mu.Unlock()

	u.updateCert(key)

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
	delete(u.secrets, secretKey{sobj.Namespace, sobj.Name})
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

func (u *secUpdater) getCert(key secretKey) (*tls.Certificate, error) {
	sec := u.getSecret(key.namespace, key.name)
	certBytes := sec[`tls.crt`]
	if len(certBytes) == 0 {
		// key or cert not specified
		return nil, fmt.Errorf("no tls.crt in secret")
	}

	keyBytes := sec[`tls.key`]
	if len(keyBytes) == 0 {
		return nil, fmt.Errorf("no tls.key in secret")
	}

	newcert, err := tls.X509KeyPair(certBytes, keyBytes)
	if err != nil {
		klog.Errorf("keypair error for %s, %v", key, err)
		return nil, err
	}
	newcert.Leaf, err = x509.ParseCertificate(newcert.Certificate[0])
	if err != nil {
		klog.Errorf("parse leaf error for %s, %v", key, err)
		return nil, err
	}

	return &newcert, nil
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
	// TODO remove circular depedency, merge certmap and secUpdater
	c.certMap = &certMap{secs: upd}
	c.secs.certMap = c.certMap

	return nil
}
