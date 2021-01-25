package minke

import (
	"crypto/tls"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"strconv"

	"k8s.io/klog/v2"
)

type httpError struct {
	status     int
	logMessage string
}

type httpRedirect struct {
	destination string
}

func (c *Controller) handler(w http.ResponseWriter, req *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			switch err := err.(type) {
			case httpRedirect:
				http.Redirect(w, req, err.destination, http.StatusMovedPermanently)
			case httpError:
				klog.Errorf("proxy: %v", err.logMessage)
				w.WriteHeader(err.status)
				return
			}
			klog.Errorf("proxy: %v", err)
		}
	}()
	c.proxy.ServeHTTP(w, req)
}

func (c *Controller) getTarget(req *http.Request) (serviceAddr, string) {
	ing, rule := c.ings.matchRule(req)
	if ing == nil {
		panic(httpError{status: http.StatusNotFound, logMessage: "no service for thing"})
	}

	if ing.httpRedir && req.TLS == nil {
		req.URL.Scheme = "https"
		req.URL.Host = req.Host
		panic(httpRedirect{destination: req.URL.String()})
	}

	port := c.svc.getServicePortScheme(rule.backend)

	eps := c.eps.getActiveAddrs(rule.backend)

	if len(eps) == 1 {
		return eps[0], port
	}

	if len(eps) > 1 {
		// TODO: this need to do the balancing thing
		return eps[rand.Intn(len(eps)-1)], port
	}

	panic(httpError{
		status:     http.StatusBadGateway,
		logMessage: fmt.Sprintf("no active endpoints for %v", rule.backend)})
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

// GetCertificate selects a cert from an ingress if one is available.
func (c *Controller) GetCertificate(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if info.ServerName == "" {
		return nil, nil
	}

	return c.certMap.GetCertificate(info)
}

// GetClientCertificate selects a cert from an ingress if one is available.
func (c *Controller) GetClientCertificate(info *tls.CertificateRequestInfo) (*tls.Certificate, error) {
	c.clientTLSCertificateMutex.RLock()
	defer c.clientTLSCertificateMutex.RUnlock()
	return c.clientTLSCertificate, nil
}
