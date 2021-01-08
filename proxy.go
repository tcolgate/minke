package minke

import (
	"crypto/tls"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strconv"
)

type httpTransport struct {
	base  http.RoundTripper
	http2 http.RoundTripper
}

func (t *httpTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	switch r.URL.Scheme {
	// This relies on an implementation detail of the http2 client, as long
	// as we've got a port int he URL, the scheme is ignored.
	case "http2", "grpc":
		r.URL.Scheme = "http"
		log.Printf("r %#v", r)
		return t.http2.RoundTrip(r)
	default:
		return t.base.RoundTrip(r)
	}
}

func (c *Controller) getTarget(req *http.Request) (serviceAddr, string) {
	var ok bool

	key, ok := c.ings.getServiceKey(req)
	if !ok {
		return serviceAddr{}, ""
	}

	port := c.svc.getServicePortScheme(key)

	eps := c.eps.getActiveAddrs(key)

	if len(eps) == 1 {
		return eps[0], port
	}

	if len(eps) > 1 {
		// TODO: this need to do the balancing thing
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

// GetCertificate selects a cert from an ingress if one is available.
func (c *Controller) GetCertificate(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if info.ServerName == "" {
		return nil, nil
	}

	cert, _ := c.ings.GetCertificate(info)
	if cert == nil {
		c.defaultTLSCertificateMutex.RLock()
		c.defaultTLSCertificateMutex.RUnlock()
		return c.defaultTLSCertificate, nil
	}
	return cert, nil
}

// GetClientCertificate selects a cert from an ingress if one is available.
func (c *Controller) GetClientCertificate(info *tls.CertificateRequestInfo) (*tls.Certificate, error) {
	c.clientTLSCertificateMutex.RLock()
	defer c.clientTLSCertificateMutex.RUnlock()
	return c.clientTLSCertificate, nil
}
