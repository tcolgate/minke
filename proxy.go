package minke

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"strings"
)

func (c *Controller) getCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return nil, nil
}

func (c *Controller) getTarget(req *http.Request) *url.URL {
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
