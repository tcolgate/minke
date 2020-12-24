package main

import (
	"context"
	"crypto/tls"
	"flag"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"golang.org/x/sync/errgroup"

	klog "k8s.io/klog/v2"

	"github.com/lucas-clemente/quic-go/http3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tcolgate/minke"
)

func init() {
}

var (
	masterURL  = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")

	adminAddr = flag.String("addr.admin", ":8080", "address to provide metrics")
	httpAddr  = flag.String("addr.http", ":80", "address to serve http")
	httpsAddr = flag.String("addr.https", ":443", "address to server http/http2/quic")

	defaultCert  = flag.String("tls.default.cert", "cert.pem", "location of default cert")
	defaultKey   = flag.String("tls.default.key", "key.pem", "location of default key")
	clientCACert = flag.String("tls.clientca", "", "ca to accept client connection from")

	defaultClientCA = flag.String("tls.client.cacert", "", "ca to trust for client connections")

	defaultClientCert = flag.String("tls.client.cert", "", "location cert to present for https client")
	defaultClientKey  = flag.String("tls.client.key", "", "location key to use for https client")
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	stop := setupSignalHandler()

	cfg, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	adminMux := http.NewServeMux()

	registry := prometheus.NewRegistry()

	registry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	registry.MustRegister(prometheus.NewGoCollector())
	metricsprovider := minke.NewPrometheusMetrics(registry)
	minke.SetProvider(metricsprovider)

	adminMux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		panic(err.Error())
	}

	ctrl, err := minke.New(clientset)
	if err != nil {
		log.Fatalf("error creating controller, err = %v", err)
		return
	}

	adminMux.Handle("/livez", http.HandlerFunc(ctrl.ServeLivezHTTP))
	adminMux.Handle("/readyz", http.HandlerFunc(ctrl.ServeReadyzHTTP))
	adminMux.HandleFunc("/debug/pprof/", pprof.Index)
	adminMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	adminMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	adminMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	adminMux.HandleFunc("/debug/pprof/trace", pprof.Trace)

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

	server := &http.Server{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		Addr:         *httpAddr,
		Handler:      ctrl,
	}

	g.Go(func() error {
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

	tlsServer := &http.Server{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		Addr:         *httpsAddr,
		Handler:      ctrl,
		TLSConfig:    tlsConfig,
	}
	g.Go(func() error {
		tlsl, err := tls.Listen("tcp", *httpsAddr, tlsConfig)
		if err != nil {
			log.Fatal(err)
		}
		tlsServer.Serve(tlsl)
		defer tlsl.Close()
		return err
	})

	http3server := http3.Server{
		Server: tlsServer,
	}

	g.Go(func() error {
		return http3server.ListenAndServe()
	})

	<-ctx.Done()
	http3server.CloseGracefully(5 * time.Second)
	server.Shutdown(context.Background())
	if err := ctx.Err(); err != nil {
		os.Exit(1)
	}
}
