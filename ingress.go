package minke

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog"

	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	listnetworkingv1beta1 "k8s.io/client-go/listers/networking/v1beta1"

	"k8s.io/client-go/tools/cache"
)

type ingressSet struct {
	sync.RWMutex
	set map[string]ingressHostGroup
}

type ingressHostGroup []ingress

type ingressGroupByPriority []ingress

type ingress struct {
	name           string
	namespace      string
	priority       *int
	defaultBackend *serviceKey
	rules          []ingressRule
	certKey        secretKey
}

type ingressRule struct {
	host     string
	re       *regexp.Regexp
	pathType string
	prefix   string
	backend  serviceKey
}

func (g ingressGroupByPriority) Len() int {
	return len(g)
}

func (g ingressGroupByPriority) Swap(i, j int) {
	g[i], g[j] = g[j], g[i]
}

func (g ingressGroupByPriority) Less(i, j int) bool {
	if g[i].priority == nil && g[j].priority != nil {
		return true
	}

	if g[i].priority != nil && g[j].priority == nil {
		return false
	}

	if g[i].priority != nil &&
		g[j].priority != nil &&
		*g[i].priority < *g[j].priority {
		return true
	}

	if g[i].priority != nil &&
		g[j].priority != nil &&
		*g[i].priority > *g[j].priority {
		return false
	}

	if g[i].name < g[j].name {
		return true
	}

	if g[i].name > g[j].name {
		return false
	}

	if g[i].namespace < g[j].namespace {
		return true
	}

	return false
}

func (ir *ingressRule) matchRule(r *http.Request) (int, bool) {
	ms := ir.re.FindStringSubmatch(r.URL.Path)
	if len(ms) == 0 {
		return 0, false
	}
	return len(ms[0]), true
}

func (ings ingressHostGroup) getServiceKey(r *http.Request) (serviceKey, bool) {
	var defBackend *serviceKey

	for i := range ings {
		if defBackend != nil {
			defBackend = ings[i].defaultBackend
		}

		var matched serviceKey
		var matchLen int
		for _, rule := range ings[i].rules {
			if l, ok := rule.matchRule(r); ok && l > matchLen {
				matched = rule.backend
				matchLen = l
			}
		}

		if matched.name != "" {
			return matched, true
		}
	}
	if defBackend != nil {
		return *defBackend, true
	}

	return serviceKey{}, false
}

func (is *ingressSet) getServiceKey(r *http.Request) (serviceKey, bool) {
	is.RLock()
	defer is.RUnlock()

	if is == nil {
		return serviceKey{}, false
	}

	ings, _ := is.set[r.Host]
	if key, ok := ings.getServiceKey(r); ok {
		return key, ok
	}

	if len(r.Host) > 0 {
		name := strings.Split(r.Host, ".")
		name[0] = "*"
		wildcardName := strings.Join(name, ".")
		ings, _ := is.set[wildcardName]
		if key, ok := ings.getServiceKey(r); ok {
			return key, ok
		}
	}

	ings, _ = is.set[""]
	if key, ok := ings.getServiceKey(r); ok {
		return key, ok
	}

	return serviceKey{}, false
}

func (is *ingressSet) getCertSecret(info *tls.ClientHelloInfo) (secretKey, error) {
	is.RLock()
	defer is.RUnlock()

	// TODO: gibberish, must pick cert properly
	h := is.set[info.ServerName]
	if len(h) != 0 {
		return h[0].certKey, nil
	}

	if len(info.ServerName) > 0 {
		name := strings.Split(info.ServerName, ".")
		name[0] = "*"
		wildcardName := strings.Join(name, ".")
		h := is.set[wildcardName]
		if len(h) > 0 {
			return h[0].certKey, nil
		}
	}

	return secretKey{}, nil
}

type ingUpdater struct {
	c *Controller
}

func (c *Controller) ourClass(ing *networkingv1beta1.Ingress) bool {
	class, _ := ing.ObjectMeta.Annotations["kubernetes.io/ingress.class"]

	if ing.Spec.IngressClassName != nil {
		// TODO: not really how we should use this, there
		// should be an IngressClass object
		class = *ing.Spec.IngressClassName
	}

	switch {
	// If we have a class set, only match our own.
	case c.class != "" && class != c.class:
		return false
	// If we have no class set, only ingresses with no class.
	case c.class == "" && class != "":
		return false
	default:
		return true
	}
}

func (u *ingUpdater) addItem(obj interface{}) error {
	ing, ok := obj.(*networkingv1beta1.Ingress)
	if !ok {
		return fmt.Errorf("interface was not an ingress %T", obj)
	}

	klog.Infof("ingress added, %s/%s", ing.GetNamespace(), ing.GetName())

	if !u.c.ourClass(ing) {
		return nil
	}

	newset := make(map[string]ingressHostGroup)
	for _, ingr := range ing.Spec.Rules {
		ning := ingress{
			name:      ing.ObjectMeta.Name,
			namespace: ing.ObjectMeta.Namespace,
		}
		if ing.Spec.Backend != nil {
			key := backendToServiceKey(ing.ObjectMeta.Namespace, ing.Spec.Backend)
			ning.defaultBackend = &key
		}

		for _, ingp := range ingr.HTTP.Paths {
			path := "^/.*"
			if ingp.Path != "" {
				if ingp.Path[0] != '/' {
					// TODO: log an error
					continue
				}
				path = "^" + ingp.Path
			}
			re, err := regexp.CompilePOSIX(path)
			if err != nil {
				// TODO: log an error
				continue
			}
			nir := ingressRule{
				host:     ingr.Host,
				prefix:   ingp.String(),
				re:       re,
				backend:  backendToServiceKey(ing.ObjectMeta.Namespace, &ingp.Backend),
				pathType: ingr.HTTP.Paths[0].String(),
			}
			ning.rules = append(ning.rules, nir)
		}
		old, _ := newset[ingr.Host]
		newset[ingr.Host] = append(old, ning)
	}

	u.c.ings.Lock()
	defer u.c.ings.Unlock()

	if u.c.ings.set == nil {
		u.c.ings.set = make(map[string]ingressHostGroup)
	}

	for n := range u.c.ings.set {
		var nings ingressHostGroup
		for i := range u.c.ings.set[n] {
			if u.c.ings.set[n][i].name == ing.ObjectMeta.Name &&
				u.c.ings.set[n][i].namespace == ing.ObjectMeta.Namespace {
				continue
			}
			nings = append(nings, u.c.ings.set[n][i])
		}
		u.c.ings.set[n] = nings
	}

	for n := range newset {
		hg := append(u.c.ings.set[n], newset[n]...)
		sort.Sort(ingressGroupByPriority(hg))
		u.c.ings.set[n] = hg
	}

	return nil
}

func (u *ingUpdater) delItem(obj interface{}) error {
	ing, ok := obj.(*networkingv1beta1.Ingress)
	if !ok {
		return fmt.Errorf("interface was not an ingress %T", obj)
	}

	klog.Infof("ingress removed, %s/%s", ing.GetNamespace(), ing.GetName())

	u.c.ings.Lock()
	defer u.c.ings.Unlock()

	for n := range u.c.ings.set {
		var nings ingressHostGroup
		for i := range u.c.ings.set[n] {
			if u.c.ings.set[n][i].name == ing.ObjectMeta.Name &&
				u.c.ings.set[n][i].namespace == ing.ObjectMeta.Namespace {
				continue
			}
			nings = append(nings, u.c.ings.set[n][i])
		}
		u.c.ings.set[n] = nings
	}

	return nil
}

func (c *Controller) setupIngProcess(ctx context.Context) error {
	upd := &ingUpdater{c}

	c.ingProc = makeProcessor(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.LabelSelector = c.selector.String()
				return c.client.NetworkingV1beta1().Ingresses(c.namespace).List(ctx, options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = c.selector.String()
				return c.client.NetworkingV1beta1().Ingresses(c.namespace).Watch(ctx, options)
			},
		},
		&networkingv1beta1.Ingress{},
		c.refresh,
		upd,
	)

	c.ingList = listnetworkingv1beta1.NewIngressLister(c.ingProc.informer.GetIndexer())

	return nil
}
