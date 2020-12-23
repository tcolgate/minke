package minke

import (
	"crypto/tls"
)

// GetCertificate selects a cert from an ingress if one is available.
func (c *Controller) GetCertificate(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if info.ServerName == "" {
		return nil, nil
	}

	// TODO: this is rubbish, need proper wildccard support.
	// and should probably pick the first valid cert.
	hg, ok := c.ings[info.ServerName]
	if !ok || len(hg) == 0 {
		return nil, nil
	}
	return hg[0].cert, nil
}
