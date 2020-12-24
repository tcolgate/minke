package minke

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	corev1 "k8s.io/api/core/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

func TestTest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs, _ := httputil.DumpRequest(r, true)
		t.Logf("request: %s", string(rs))
		fmt.Fprintln(w, "OK")
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	cp, _ := strconv.Atoi(u.Port())
	clientset := fake.NewSimpleClientset(
		&networkingv1beta1.Ingress{
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
			Spec: networkingv1beta1.IngressSpec{
				Rules: []networkingv1beta1.IngressRule{
					{
						Host: "blah",
						IngressRuleValue: networkingv1beta1.IngressRuleValue{
							HTTP: &networkingv1beta1.HTTPIngressRuleValue{
								Paths: []networkingv1beta1.HTTPIngressPath{
									{
										Backend: networkingv1beta1.IngressBackend{
											ServiceName: "first",
											ServicePort: intstr.FromString("mysvc"),
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
		&networkingv1beta1.Ingress{
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
			Spec: networkingv1beta1.IngressSpec{
				Rules: []networkingv1beta1.IngressRule{
					{
						Host: "blah",
						IngressRuleValue: networkingv1beta1.IngressRuleValue{
							HTTP: &networkingv1beta1.HTTPIngressRuleValue{
								Paths: []networkingv1beta1.HTTPIngressPath{
									{
										Backend: networkingv1beta1.IngressBackend{
											ServiceName: "first",
											ServicePort: intstr.FromString("mysvc"),
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
func TestWebsocket(t *testing.T) {
	var upgrader = websocket.Upgrader{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs, _ := httputil.DumpRequest(r, true)
		t.Logf("ws request: %s", string(rs))

		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal("ws server upgrade:", err)
			return
		}
		defer c.Close()
		for {
			mt, message, err := c.ReadMessage()
			if err != nil {
				t.Log("ws server read:", err)
				break
			}
			t.Logf("recv: %s", message)
			err = c.WriteMessage(mt, message)
			if err != nil {
				t.Fatal("ws server write:", err)
			}
		}
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	cp, _ := strconv.Atoi(u.Port())
	clientset := fake.NewSimpleClientset(
		&networkingv1beta1.Ingress{
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
			Spec: networkingv1beta1.IngressSpec{
				Rules: []networkingv1beta1.IngressRule{
					{
						Host: "blah",
						IngressRuleValue: networkingv1beta1.IngressRuleValue{
							HTTP: &networkingv1beta1.HTTPIngressRuleValue{
								Paths: []networkingv1beta1.HTTPIngressPath{
									{
										Backend: networkingv1beta1.IngressBackend{
											ServiceName: "first",
											ServicePort: intstr.FromString("mysvc"),
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
	defer cancel()
	go ctrl.Run(ctx.Done())
	time.Sleep(1 * time.Second)

	pts := httptest.NewServer(ctrl)
	defer pts.Close()

	h := http.Header{}
	h.Set("Host", "blah")
	ctrlu, _ := url.Parse(pts.URL)
	ctrlu.Scheme = "ws"
	ctrlu.Path = "/"
	t.Logf("ws url: %v", ctrlu.String())

	c, _, err := websocket.DefaultDialer.DialContext(ctx, ctrlu.String(), h)
	if err != nil {
		t.Fatal("ws client dial:", err)
	}

	for i := 0; i < 10; i++ {
		msg := fmt.Sprintf("message %v", i)
		err = c.WriteMessage(websocket.TextMessage, []byte(msg))
		if err != nil {
			t.Fatalf("ws client write, %v", err)
			return
		}

		_, message, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("ws client read failed, %v", err)
			return
		}
		if string(message) != msg {
			t.Fatalf("read expected %q, got %q", msg, message)
		}
	}
	c.Close()

}

func TestHTTP2Backend(t *testing.T) {
	h2s := &http2.Server{}

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "OK")
	})

	ts := httptest.NewServer(h2c.NewHandler(h, h2s))
	defer ts.Close()

	http2.ConfigureServer(ts.Config, &http2.Server{})
	strhttp2 := "HTTP2"

	u, _ := url.Parse(ts.URL)
	cp, _ := strconv.Atoi(u.Port())
	clientset := fake.NewSimpleClientset(
		&networkingv1beta1.Ingress{
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
			Spec: networkingv1beta1.IngressSpec{
				Rules: []networkingv1beta1.IngressRule{
					{
						Host: "blah",
						IngressRuleValue: networkingv1beta1.IngressRuleValue{
							HTTP: &networkingv1beta1.HTTPIngressRuleValue{
								Paths: []networkingv1beta1.HTTPIngressPath{
									{
										Backend: networkingv1beta1.IngressBackend{
											ServiceName: "first",
											ServicePort: intstr.FromString("mysvc"),
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
				Ports: []corev1.ServicePort{
					{Name: "mysvc", AppProtocol: &strhttp2},
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
	defer cancel()
	go ctrl.Run(ctx.Done())
	time.Sleep(1 * time.Second)

	pts := httptest.NewServer(ctrl)
	defer pts.Close()

	ctrlu, _ := url.Parse(pts.URL)
	ctrlu.Scheme = "http"
	ctrlu.Path = "/"
	t.Logf("http2 http url: %v", ctrlu.String())

	req, _ := http.NewRequest("GET", pts.URL+"/hello", nil)
	req.Host = "blah"
	resp, err := pts.Client().Do(req)
	if err != nil {
		t.Fatalf("got error %v", err)
	}
	defer resp.Body.Close()
	t.Logf("http2 http resp: %v", resp)

	bs, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Logf("http2 http read resp: %v", err)
	}

	t.Logf("http2 http resp body: %s", bs)
}
