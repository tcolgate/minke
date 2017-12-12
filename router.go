package minke

import (
	"crypto/tls"
)

func (c *Controller) GetCertificate(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return nil, nil
}
