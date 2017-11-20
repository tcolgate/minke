package minke

import (
	"context"
	"testing"

	extv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	ctrl, err := New(clientset)
	if err != nil {
		t.Fatalf("error creating controller, err = %v", err)
		return
	}

	ctrl.Run(context.Background())
}
