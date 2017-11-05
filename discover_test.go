package minke

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func TestTest(t *testing.T) {
	/*
		// creates the in-cluster config
		config, err := rest.InClusterConfig()
		if err != nil {
			panic(err.Error())
		}
	*/

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

	inff := informers.NewSharedInformerFactory(client, 15*time.Second)

	ctrl := New(inff, nil)
	ctrl.Run(ctx)
}
