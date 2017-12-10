package main

import (
	"context"
	"crypto/tls"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"golang.org/x/sync/errgroup"

	extv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tcolgate/minke"
)

var (
	adminAddr = flag.String("addr.admin", ":8080", "address to provide metrics")
	httpAddr  = flag.String("addr.http", ":80", "address to provide metrics")
	httpsAddr = flag.String("addr.https", ":443", "address to provide metrics")

	defaultCert = flag.String("tls.default.cert", "cert.pem", "location of default cert")
	defaultKey  = flag.String("tls.default.key", "key.pem", "location of default key")
)

func main() {

	adminMux := http.NewServeMux()

	registry := prometheus.NewRegistry()

	registry.MustRegister(prometheus.NewProcessCollector(os.Getpid(), ""))
	registry.MustRegister(prometheus.NewGoCollector())
	metricsprovider := minke.NewPrometheusMetrics(registry)
	minke.SetProvider(metricsprovider)

	adminMux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	clientset := fake.NewSimpleClientset(
		&extv1beta1.Ingress{
			TypeMeta: metav1.TypeMeta{
				Kind: "Ingress",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "first",
				Namespace: "default",
			},
			Spec: extv1beta1.IngressSpec{},
		},
	)

	clientset.ExtensionsV1beta1().Ingresses("default").Create(
		&extv1beta1.Ingress{
			TypeMeta: metav1.TypeMeta{
				Kind: "Ingress",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "second",
				Namespace: "default",
			},
			Spec: extv1beta1.IngressSpec{},
		},
	)

	ctrl, err := minke.New(clientset)
	if err != nil {
		log.Fatalf("error creating controller, err = %v", err)
		return
	}

	adminMux.Handle("/healthz", http.HandlerFunc(ctrl.ServeHealthzHTTP))

	stop := make(chan struct{})
	go ctrl.Run(stop)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		err := http.ListenAndServe(*adminAddr, adminMux)
		if err != nil {
			log.Printf("http listener error, %v", err)
		}
		return err
	})

	g.Go(func() error {
		server := &http.Server{
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			Addr:         *httpAddr,
			Handler:      ctrl,
		}
		err := server.ListenAndServe()
		if err != nil {
			log.Printf("http listener error, %v", err)
		}
		return err
	})

	cert, err := tls.LoadX509KeyPair(*defaultCert, *defaultKey)
	if err != nil {
		log.Println(err)
		return
	}

	tlsConfig := &tls.Config{
		PreferServerCipherSuites: true,
		CurvePreferences: []tls.CurveID{
			tls.CurveP256,
			tls.X25519, // Go 1.8 only
		},
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305, // Go 1.8 only
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,   // Go 1.8 only
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,

			// Best disabled, as they don't provide Forward Secrecy,
			// but might be necessary for some clients
			// tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			// tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
		},
		Certificates:   []tls.Certificate{cert},
		GetCertificate: ctrl.GetCertificate,
	}

	g.Go(func() error {
		tlsServer := &http.Server{
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			Addr:         *httpsAddr,
			Handler:      ctrl,
			TLSConfig:    tlsConfig,
		}
		tlsl, err := tls.Listen("tcp", *httpsAddr, tlsConfig)
		if err != nil {
			log.Fatal(err)
		}
		tlsServer.Serve(tlsl)
		defer tlsl.Close()
		return err
	})

	<-ctx.Done()
	if err := ctx.Err(); err != nil {
		os.Exit(1)
	}
}
