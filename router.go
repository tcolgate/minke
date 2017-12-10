package minke

import (
	"crypto/tls"
	"log"
)

func (c *Controller) GetCertificate(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
	log.Println(info)
	return nil, nil
}
