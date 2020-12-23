package minke

import (
	"math/rand"
	"net"
	"net/http"
	"strconv"
)

func (c *Controller) getTarget(req *http.Request) (serviceAddr, string) {
	var ok bool
	c.mutex.RLock()
	ings := c.ings
	epss := c.eps
	c.mutex.RUnlock()

	key, ok := ings.getServiceKey(req)
	if !ok {
		return serviceAddr{}, ""
	}

	port := c.svc.getServicePortScheme(key)

	eps, _ := epss[key]

	if len(eps) == 1 {
		return eps[0], port
	}

	if len(eps) > 1 {
		return eps[rand.Intn(len(eps)-1)], port
	}

	return serviceAddr{}, port
}

func (c *Controller) director(req *http.Request) {
	target, scheme := c.getTarget(req)
	req.URL.Host = net.JoinHostPort(target.addr, strconv.Itoa(target.port))
	req.URL.Scheme = scheme

	if _, ok := req.Header["User-Agent"]; !ok {
		// explicitly disable User-Agent so it's not set to default value
		req.Header.Set("User-Agent", "")
	}
}
