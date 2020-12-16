package minke

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	corev1 "k8s.io/api/core/v1"
	extv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

func TestTest(t *testing.T) {
	/*
		// creates the in-cluster config
		config, err := rest.InClusterConfig()
		if err != nil {
			panic(err.Error())
		}
	*/

	/*
		sigs := make(chan os.Signal, 1)
		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			s := <-sigs
			fmt.Println("Got signal:", s)
			cancel()
		}()

		// use the current context in kubeconfig
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			panic(err.Error())
		}
		// creates the clientset
		clientset, err := kubernetes.NewForConfig(config)
	*/

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs, _ := httputil.DumpRequest(r, true)
		t.Logf("request: %s", string(rs))
		fmt.Fprintln(w, "OK")
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	cp, _ := strconv.Atoi(u.Port())
	clientset := fake.NewSimpleClientset(
		&extv1beta1.Ingress{
			TypeMeta: metav1.TypeMeta{
				Kind: "Ingress",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "first",
				Namespace: "default",
				Annotations: map[string]string{
					"kubernetes.io/ingress.class": "minke",
				},
			},
			Spec: extv1beta1.IngressSpec{
				Rules: []extv1beta1.IngressRule{
					{
						Host: "blah",
						IngressRuleValue: extv1beta1.IngressRuleValue{
							HTTP: &extv1beta1.HTTPIngressRuleValue{
								Paths: []extv1beta1.HTTPIngressPath{
									{
										Backend: extv1beta1.IngressBackend{
											ServiceName: "first",
											ServicePort: intstr.Parse("mysvc"),
										},
									},
								},
							},
						},
					},
				},
			},
		},
		&corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind: "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "first",
				Namespace: "default",
				Annotations: map[string]string{
					"service.alpha.kubernetes.io/app-protocol": `{"mysvc":"HTTP"}`,
				},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{Name: "mysvc"},
				},
			},
		},
		&corev1.Endpoints{
			TypeMeta: metav1.TypeMeta{
				Kind: "Endpoints",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "first",
			},
			Subsets: []corev1.EndpointSubset{
				{
					Addresses: []corev1.EndpointAddress{
						{IP: u.Hostname()},
						{IP: u.Hostname()},
						{IP: u.Hostname()},
						{IP: u.Hostname()},
					},
					Ports: []corev1.EndpointPort{
						{Name: "mysvc", Port: int32(cp)},
					},
				},
			},
		},
	)

	ctrl, err := New(clientset)
	if err != nil {
		t.Fatalf("error creating controller, err = %v", err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	go ctrl.Run(ctx.Done())
	time.Sleep(1 * time.Second)

	pts := httptest.NewServer(ctrl)
	defer pts.Close()

	req, _ := http.NewRequest("GET", pts.URL+"/hello", nil)
	req.Host = "blah"
	resp, err := pts.Client().Do(req)
	t.Logf("resp: %#v, err: %v", resp, err)

	cancel()
}

func BenchmarkMinke(b *testing.B) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "OK")
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	cp, _ := strconv.Atoi(u.Port())
	clientset := fake.NewSimpleClientset(
		&extv1beta1.Ingress{
			TypeMeta: metav1.TypeMeta{
				Kind: "Ingress",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "first",
				Namespace: "default",
				Annotations: map[string]string{
					"kubernetes.io/ingress.class": "minke",
				},
			},
			Spec: extv1beta1.IngressSpec{
				Rules: []extv1beta1.IngressRule{
					{
						Host: "blah",
						IngressRuleValue: extv1beta1.IngressRuleValue{
							HTTP: &extv1beta1.HTTPIngressRuleValue{
								Paths: []extv1beta1.HTTPIngressPath{
									{
										Backend: extv1beta1.IngressBackend{
											ServiceName: "first",
											ServicePort: intstr.Parse("mysvc"),
										},
									},
								},
							},
						},
					},
				},
			},
		},
		&corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind: "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "first",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{},
			},
		},
		&corev1.Endpoints{
			TypeMeta: metav1.TypeMeta{
				Kind: "Endpoints",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "first",
			},
			Subsets: []corev1.EndpointSubset{
				{
					Addresses: []corev1.EndpointAddress{
						{IP: u.Hostname()},
						{IP: u.Hostname()},
						{IP: u.Hostname()},
						{IP: u.Hostname()},
					},
					Ports: []corev1.EndpointPort{
						{Name: "mysvc", Port: int32(cp)},
					},
				},
			},
		},
	)

	mp := NewPrometheusMetrics(prometheus.NewRegistry())
	ctrl, err := New(clientset, WithMetricsProvider(mp))
	if err != nil {
		b.Fatalf("error creating controller, err = %v", err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	go ctrl.Run(ctx.Done())
	time.Sleep(1 * time.Second)

	pts := httptest.NewServer(ctrl)
	defer pts.Close()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("GET", pts.URL+"/hello", nil)
		req.Host = "blah"
		resp, err := pts.Client().Do(req)
		if err != nil {
			b.Errorf("got error %v", err)
			continue
		}
		resp.Body.Close()
	}

	cancel()
}

func BenchmarkReverseProxy(b *testing.B) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "OK")
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	pts := httptest.NewServer(httputil.NewSingleHostReverseProxy(u))
	defer pts.Close()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("GET", pts.URL+"/hello", nil)
		req.Host = "blah"
		resp, err := ts.Client().Do(req)
		if err != nil {
			b.Errorf("got error %v", err)
			continue
		}
		resp.Body.Close()
	}
}

func BenchmarkNoProxy(b *testing.B) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "OK")
	}))
	defer ts.Close()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("GET", ts.URL+"/hello", nil)
		req.Host = "blah"
		resp, err := ts.Client().Do(req)
		if err != nil {
			b.Errorf("got error %v", err)
			continue
		}
		resp.Body.Close()
	}
}
