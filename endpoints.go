package minke

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	listcorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

type epsUpdater struct {
}

func (*epsUpdater) addItem(obj interface{}) error {
	return nil
}

func (*epsUpdater) delItem(obj interface{}) error {
	return nil
}

func (c *Controller) setupEndpointsProcess() error {
	upd := &epsUpdater{}

	c.epsProc = makeProcessor(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.client.CoreV1().Endpoints(metav1.NamespaceAll).List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.client.CoreV1().Endpoints(metav1.NamespaceAll).Watch(options)
			},
		},
		&corev1.Endpoints{},
		c.refresh,
		upd,
	)

	c.epsList = listcorev1.NewEndpointsLister(c.epsProc.informer.GetIndexer())

	return nil
}

func (c *Controller) processEndpointsItem(string) error {
	return nil
}
