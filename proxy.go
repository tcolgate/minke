package minke

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
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

func (c *Controller) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) {
		klog.Infof("client cancelled: %#v", err)
		return
	}

	klog.Infof("proxy backend error: %#v", err)
	w.WriteHeader(http.StatusBadGateway)
}

func (c *Controller) handler(w http.ResponseWriter, req *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			switch err := err.(type) {
			case httpRedirect:
				http.Redirect(w, req, err.destination, http.StatusMovedPermanently)
				return
			case httpError:
				klog.Errorf("proxy: %v", err.logMessage)
				w.WriteHeader(err.status)
				return
			default:
				klog.Errorf("proxy error: %+v", err)
				w.WriteHeader(http.StatusBadGateway)
				return
			}
		}
	}()

	if c.setquicheaders != nil {
		c.setquicheaders(w.Header())
	}
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

	ep := c.eps.getNextAddr(rule.backend)
	if ep.addr == "" {
		panic(httpError{
			status:     http.StatusBadGateway,
			logMessage: fmt.Sprintf("no active endpoints for %v", rule.backend)})
	}
	return ep, port
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
