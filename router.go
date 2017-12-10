package minke

import (
	"crypto/tls"
	"log"
	"net/http"
)

func (c *Controller) ServeHTTP(w http.ResponseWriter, r *http.Request) {
}

func (c *Controller) GetCertificate(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
	log.Println(info)
	return nil, nil
}
