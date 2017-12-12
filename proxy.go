package minke

import (
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
)

func (c *Controller) getTarget(req *http.Request) *url.URL {
	var ings []ingress
	var ok bool
	log.Printf("REQUEST: %#v", *req)
	log.Printf("INGRESSES: %#v", c.ings)
	log.Printf("ENDPOINTS: %#v", c.eps)
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
		log.Printf("checking ingress %#v", i)
		for j := range ings[i].rules {
			log.Printf("checking ingress rule path %#v against %#v %#v %v", req.URL.Path, i, j, ings[i].rules[j].re)
			if ings[i].rules[j].re.MatchString(req.URL.Path) {
				key := ings[i].rules[j].backend
				log.Printf("KEY %#v", key)
				for k := range c.eps[key] {
					urls = append(urls, c.eps[key][k])
				}
				break FindURLS
			}
		}
	}

	log.Printf("SELECTED URLS: %#v", urls)

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
	log.Printf("target: %#v", target)
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
