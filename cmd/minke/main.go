package main

import (
	"context"
	"crypto/tls"
	"flag"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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

	namespace = flag.String("namespace", metav1.NamespaceAll, "namespace to watch resources")
	selector  = flag.String("l", "", "label selector to match ingresses")
	class     = flag.String("class", "minke", "ingress class to match")

	adminAddr = flag.String("addr.admin", ":8080", "address to provide metrics")
	httpAddr  = flag.String("addr.http", ":80", "address to serve http")
	httpsAddr = flag.String("addr.https", ":443", "address to server http/http2/quic")

	httpRedir = flag.Bool("http.redirect-https", true, "What should the default http redirect bahviour be")

	serverTLSDefaultSecrets = flag.String("tls.server.default.secrets", "", "comma separated list of the NAMESPACE/NAME of the default TLS secrets")
	serverTLSClientCASecret = flag.String("tls.server.clientca.secret", "", "")

	clientTLSSecret = flag.String("tls.client.secret", "", "location cert to present for https client")
	clientTLSCA     = flag.String("tls.client.ca.secret", "", "CA to trust for client connections")
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	klog.CopyStandardLogTo("ERROR")

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

	selector, err := labels.Parse(*selector)
	if err != nil {
		panic(err.Error())
	}

	ciphers := []uint16{
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
	}
	curves := []tls.CurveID{
		tls.CurveP256,
		tls.X25519, // Go 1.8 only
	}
	tlsMinVersion := uint16(tls.VersionTLS12)

	tlsClientConfig := &tls.Config{
		PreferServerCipherSuites: true,
		CurvePreferences:         curves,
		MinVersion:               tlsMinVersion,
		CipherSuites:             ciphers,
	}

	var defaultSecrets []string
	for _, str := range strings.Split(*serverTLSDefaultSecrets, ",") {
		defaultSecrets = append(defaultSecrets, strings.TrimSpace(str))
	}

	ctrl, err := minke.New(
		clientset,
		minke.WithNamespace(*namespace),
		minke.WithClass(*class),
		minke.WithSelector(selector),
		minke.WithDefaultHTTPRedirect(*httpRedir),
		minke.WithDefaultTLSSecrets(defaultSecrets...),
		minke.WithClientTLSConfig(tlsClientConfig),
		minke.WithClientTLSSecret(*clientTLSSecret),
	)
	if err != nil {
		log.Fatalf("error creating controller, err = %v", err)
		return
	}

	adminMux.Handle("/livez", http.HandlerFunc(ctrl.ServeLivezHTTP))
	adminMux.Handle("/readyz", http.HandlerFunc(ctrl.ServeReadyzHTTP))
	adminMux.Handle("/status", http.HandlerFunc(ctrl.ServeStatusHTTP))
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

	tlsConfig := &tls.Config{
		PreferServerCipherSuites: true,
		CurvePreferences:         curves,
		MinVersion:               tlsMinVersion,
		CipherSuites:             ciphers,
		GetCertificate:           ctrl.GetCertificate,
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
