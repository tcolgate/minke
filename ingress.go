package minke

import (
	"context"
	"encoding/json"
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

// ingressSet maps hostnames to the ingresses that use them.
type ingressSet struct {
	sync.RWMutex
	set map[string]ingressHostGroup
}

// MarshalJSON lets us report the status of the ingress set
func (is *ingressSet) MarshalJSON() ([]byte, error) {
	is.RLock()
	defer is.RUnlock()
	return json.Marshal(is.set)
}

type ingressHostGroup []ingress

type ingressGroupByPriority []ingress

type pathType int

const (
	glob pathType = iota
	prefix
	exact
	re2
)

func (pt pathType) String() string {
	switch pt {
	case glob:
		return "glob"
	case re2:
		return "re2"
	case prefix:
		return "prefix"
	case exact:
		return "exact"
	default:
		return fmt.Sprintf("(unknown:%v)", int(pt))
	}
}

func (pt pathType) MarshalJSON() ([]byte, error) {
	return json.Marshal(pt.String())
}

// an ingress here includes the set of rules for an ingress
// that match a specific host.
type ingress struct {
	name           string
	namespace      string
	priority       *int
	defaultBackend *serviceKey
	rules          []ingressRule
}

func (ing ingress) MarshalJSON() ([]byte, error) {
	strmap := map[string]interface{}{
		"ingress": fmt.Sprintf("%s/%s", ing.namespace, ing.name),
		"rules":   ing.rules,
	}
	if ing.defaultBackend != nil {
		strmap["defaultBackend"] = *ing.defaultBackend
	}
	if ing.priority != nil {
		strmap["priority"] = *ing.priority
	}
	return json.Marshal(strmap)
}

// ingress rules is one specific path, and the backend it
// maps to.
type ingressRule struct {
	host     string
	re       *regexp.Regexp
	pathType pathType
	path     string
	backend  serviceKey
}

func (ir ingressRule) MarshalJSON() ([]byte, error) {
	strmap := map[string]interface{}{
		"backend":  ir.backend,
		"path":     ir.path,
		"pathType": ir.pathType,
		"host":     ir.host,
	}
	if ir.host == "" {
		strmap["host"] = "*"
	}
	return json.Marshal(strmap)
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
	if ir.host != "" && ir.host != r.Host {
		// This should /probably/ check if ir.host is a wildcard, but
		// it's a little ambigious from the docs if they are supported here.
		return 0, false
	}
	switch ir.pathType {
	case glob:
		if strings.HasSuffix(ir.path, "/*") {
			subPath := ir.path[0 : len(ir.path)-2]
			if subPath == "" {
				subPath = "/"
			}
			if strings.HasPrefix(r.URL.Path, subPath) {
				if len(r.URL.Path) == len(subPath) {
					return len(subPath), false
				}
				if r.URL.Path[len(subPath)] == '/' {
					return len(subPath) + 1, false
				}
			}
		} else {
			if r.URL.Path == ir.path {
				return len(ir.path), false
			}
		}
	case re2:
		ms := ir.re.FindStringSubmatch(r.URL.Path)
		if len(ms) != 0 {
			return len(ms[0]), false
		}
	case exact:
		if r.URL.Path == ir.path {
			return len(ir.path), true
		}
	case prefix:
		if strings.HasPrefix(r.URL.Path, ir.path) {
			if len(r.URL.Path) == len(ir.path) {
				return len(ir.path), false
			}
			if r.URL.Path[len(ir.path)] == '/' {
				return len(ir.path) + 1, false
			}
		}
	default:
	}
	return 0, false
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
			l, exact := rule.matchRule(r)
			if exact {
				matched = rule.backend
				matchLen = l
				break
			}
			if l > matchLen {
				matched = rule.backend
				matchLen = l
				if exact {
					break
				}
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

func (is *ingressSet) update(name, namespace string, newset map[string]ingressHostGroup) {
	is.Lock()
	defer is.Unlock()

	if is.set == nil {
		is.set = make(map[string]ingressHostGroup)
	}

	for n := range is.set {
		var nings ingressHostGroup
		for i := range is.set[n] {
			if is.set[n][i].name == name &&
				is.set[n][i].namespace == namespace {
				continue
			}
			nings = append(nings, is.set[n][i])
		}
		is.set[n] = nings
	}

	for n := range newset {
		hg := append(is.set[n], newset[n]...)
		sort.Sort(ingressGroupByPriority(hg))
		is.set[n] = hg
	}
}

func (is *ingressSet) clear(name, namespace string) {
	is.Lock()
	defer is.Unlock()

	for n := range is.set {
		var nings ingressHostGroup
		for i := range is.set[n] {
			if is.set[n][i].name == name &&
				is.set[n][i].namespace == namespace {
				continue
			}
			nings = append(nings, is.set[n][i])
		}
		is.set[n] = nings
	}
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

	if !u.c.ourClass(ing) {
		return nil
	}

	klog.Infof("ingress added, %s/%s", ing.GetNamespace(), ing.GetName())

	// we'll collate  alist of hosts incase the TLS list
	// doesn't include one
	var hosts []string

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
			var re *regexp.Regexp
			var pathType pathType
			path := ingp.Path

			if ingp.PathType != nil {
				switch *ingp.PathType {
				case networkingv1beta1.PathTypePrefix:
					if path == "" {
						path = "/"
					}
					pathType = prefix
					// prefix matching ignores trailing /
					if len(path) > 1 && path[len(path)-1] == '/' {
						path = path[0 : len(path)-1]
					}
				case networkingv1beta1.PathTypeExact:
					pathType = exact
					if path == "" {
						path = "/"
					}
				case "re2":
					pathType = re2
					if path == "" {
						path = "/"
					}
					if !strings.HasPrefix(path, "^") {
						path = "^" + path
					}
					var err error
					re, err = regexp.CompilePOSIX(path)
					if err != nil {
						// todo: log an error
						continue
					}
				default:
					pathType = glob
					if path == "" {
						path = "/*"
					}
				}
			} else {
				if path == "" {
					// If the user has not specified any paths or globs, we'll use our
					// fastest wildcard match option
					pathType = prefix
					path = "/"
				} else {
					pathType = glob
				}
			}

			nir := ingressRule{
				host:     ingr.Host,
				path:     path,
				re:       re,
				backend:  backendToServiceKey(ing.ObjectMeta.Namespace, &ingp.Backend),
				pathType: pathType,
			}
			ning.rules = append(ning.rules, nir)
		}
		old, _ := newset[ingr.Host]
		newset[ingr.Host] = append(old, ning)
		if ingr.Host != "" {
			hosts = append(hosts, ingr.Host)
		}
	}

	for _, t := range ing.Spec.TLS {
		var certHosts []string
		if len(t.Hosts) == 0 {
			certHosts = hosts
		}

		secKey := secretKey{namespace: ing.Namespace, name: t.SecretName}
		ingKey := ingressKey{namespace: ing.Namespace, name: ing.Name}

		cert := u.c.secs.getCert(secKey)

		if cert != nil {
			cmapEntry := &certMapEntry{
				sec:  secKey,
				ing:  ingKey,
				cert: cert,
			}
			allcerts := make(map[string][]*certMapEntry, len(hosts))
			for _, h := range certHosts {
				allcerts[h] = [](*certMapEntry){cmapEntry}
			}
			u.c.certMap.updateIngress(ingKey, allcerts)
		}
	}

	u.c.ings.update(ing.ObjectMeta.Name, ing.ObjectMeta.Namespace, newset)

	return nil
}

func (u *ingUpdater) delItem(obj interface{}) error {
	ing, ok := obj.(*networkingv1beta1.Ingress)
	if !ok {
		return fmt.Errorf("interface was not an ingress %T", obj)
	}

	klog.Infof("ingress removed, %s/%s", ing.GetNamespace(), ing.GetName())

	u.c.ings.clear(ing.ObjectMeta.Name, ing.ObjectMeta.Namespace)

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
