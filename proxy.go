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

	certKey, _ := c.ings.getCertSecret(info)
	if certKey.namespace == "" {
		c.defaultTLSCertificateMutex.RLock()
		c.defaultTLSCertificateMutex.RUnlock()
		defCert := c.secs.getCert(secretKey{
			namespace: c.svc.c.defaultTLSSecretNamespace,
			name:      c.svc.c.defaultTLSSecretName,
		})
		log.Printf("send default cert %#v", defCert)
		return defCert, nil
	}

	return c.secs.getCert(certKey), nil
}

// GetClientCertificate selects a cert from an ingress if one is available.
func (c *Controller) GetClientCertificate(info *tls.CertificateRequestInfo) (*tls.Certificate, error) {
	c.clientTLSCertificateMutex.RLock()
	defer c.clientTLSCertificateMutex.RUnlock()
	return c.clientTLSCertificate, nil
}

/* // get certs from the stadlib
  func (c *Config) getCertificate(clientHello *ClientHelloInfo) (*Certificate, error) {
		if c.GetCertificate != nil && (len(c.Certificates) == 0 || len(clientHello.ServerName) > 0) {
			cert, err := c.GetCertificate(clientHello)
			if cert != nil || err != nil {
				return cert, err
			}
		}

		if len(c.Certificates) == 0 {
			return nil, errNoCertificates
		}

		if len(c.Certificates) == 1 {
			// There's only one choice, so no point doing any work.
			return &c.Certificates[0], nil
		}

		if c.NameToCertificate != nil {
			name := strings.ToLower(clientHello.ServerName)
			if cert, ok := c.NameToCertificate[name]; ok {
				return cert, nil
			}
			if len(name) > 0 {
				labels := strings.Split(name, ".")
				labels[0] = "*"
				wildcardName := strings.Join(labels, ".")
				if cert, ok := c.NameToCertificate[wildcardName]; ok {
					return cert, nil
				}
			}
		}

		for _, cert := range c.Certificates {
			if err := clientHello.SupportsCertificate(&cert); err == nil {
				return &cert, nil
			}
		}

		// If nothing matches, return the first certificate.
		return &c.Certificates[0], nil
	}

*/
