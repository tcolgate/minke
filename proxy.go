package minke

import (
	"math/rand"
	"net/http"
	"net/url"
	"strings"
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

	var urls []*url.URL
FindURLS:
	for i := range ings {
		for j := range ings[i].rules {
			if ings[i].rules[j].re.MatchString(req.URL.Path) {
				key := ings[i].rules[j].backend
				for k := range c.eps[key] {
					urls = append(urls, c.eps[key][k])
				}
				break FindURLS
			}
		}
	}

	if len(urls) == 1 {
		return urls[0]
	}

	if len(urls) > 1 {
		return urls[rand.Intn(len(urls)-1)]
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
