package minke

import (
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func (c *Controller) getTarget(req *http.Request) *url.URL {
	var ings []ingress
	var ok bool
	c.mutex.Lock()
	if ings, ok = c.ings[req.Host]; !ok {
		ings, ok = c.ings[""]
	}
	c.mutex.Unlock()

	if ings == nil {
		return nil
	}

	var urls []*url.URL
FindURLS:
	for i := range ings {
		for j := range ings[i].rules {
			if ings[i].rules[j].re.MatchString(req.URL.Path) {
				key := ings[i].rules[j].backend
				for k := range c.eps[key] {
					urls = append(urls, c.eps[key][k])
				}
				break FindURLS
			}
		}
	}

	if len(urls) == 1 {
		return urls[0]
	}

	if len(urls) > 1 {
		return urls[rand.Intn(len(urls)-1)]
	}

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

func websocketProxy(target string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d, err := net.Dial("tcp", target)
		if err != nil {
			http.Error(w, "Error contacting backend server.", 500)
			log.Printf("Error dialing websocket backend %s: %v", target, err)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "Not a hijacker?", 500)
			return
		}
		nc, _, err := hj.Hijack()
		if err != nil {
			log.Printf("Hijack error: %v", err)
			return
		}
		defer nc.Close()
		defer d.Close()

		err = r.Write(d)
		if err != nil {
			log.Printf("Error copying request to target: %v", err)
			return
		}

		errc := make(chan error, 2)
		cp := func(dst io.Writer, src io.Reader) {
			_, err := io.Copy(dst, src)
			errc <- err
		}
		go cp(d, nc)
		go cp(nc, d)
		<-errc
	})
}

/*
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isWebsocket(r) {
		p := websocketProxy("backendTargetURL") //example URL
		p.ServeHTTP(w, r)
		return
	}

	if h := s.handler(r); h != nil {
		h.ServeHTTP(w, r)
		return
	}
	http.Error(w, "Not found.", http.StatusNotFound)
}

func isWebsocket(req *http.Request) bool {
	conn_hdr := ""
	conn_hdrs := req.Header["Connection"]
	if len(conn_hdrs) > 0 {
		conn_hdr = conn_hdrs[0]
	}

	upgrade_websocket := false
	if strings.ToLower(conn_hdr) == "upgrade" {
		upgrade_hdrs := req.Header["Upgrade"]
		if len(upgrade_hdrs) > 0 {
			upgrade_websocket = (strings.ToLower(upgrade_hdrs[0]) == "websocket")
		}
	}

	return upgrade_websocket
}
*/
