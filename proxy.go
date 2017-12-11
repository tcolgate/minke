package minke

import (
	"net/http"
	"net/url"
	"strings"

	"k8s.io/apimachinery/pkg/util/intstr"
)

func (c *Controller) getTarget(req *http.Request) *url.URL {
	var ings []ingress
	var ok bool
	c.mutex.Lock()
	if ings, ok = c.ings[req.Host]; !ok {
		ings, ok = c.ings[""]
	}
	c.mutex.Unlock()

	if ings == nil {
		return nil
	}

	var ing ingress
	var svcName string
	var svcPort intstr.IntOrString
	for i := range ings {
		for j := range ings[i].rules {
			if ings[i].rules[j].re.MatchString(req.URL.Path) {
				ing = ings[i]
				svcName = ings[i].rules[j].svc
				svcPort = ings[i].rules[j].svcPort
			}
		}
	}

	subsets := c.epsList.Endpoints(ing.namespace).Get().Subsets
	for k := range subsets {
		subsets[k].Addresses
		for l := range subsets[k].Ports {
			subsets[k].Ports[l
		}
	}

	return nil
}

func (c *Controller) director(req *http.Request) {
	target := c.getTarget(req)
	targetQuery := target.RawQuery
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
	req.URL.Path = singleJoiningSlash(target.Path, req.URL.Path)
	if targetQuery == "" || req.URL.RawQuery == "" {
		req.URL.RawQuery = targetQuery + req.URL.RawQuery
	} else {
		req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
	}
	if _, ok := req.Header["User-Agent"]; !ok {
		// explicitly disable User-Agent so it's not set to default value
		req.Header.Set("User-Agent", "")
	}
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}
