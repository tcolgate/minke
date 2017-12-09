package minke

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	listcorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

type svcUpdater struct {
}

func (*svcUpdater) addItem(obj interface{}) error {
	return nil
}

func (*svcUpdater) delItem(obj interface{}) error {
	return nil
}

func (c *Controller) setupServicesProcess() error {
	upd := &svcUpdater{}

	c.svcProc = makeProcessor(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.client.CoreV1().Services(metav1.NamespaceAll).List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.client.CoreV1().Services(metav1.NamespaceAll).Watch(options)
			},
		},
		&corev1.Service{},
		c.refresh,
		upd,
	)

	c.svcList = listcorev1.NewServiceLister(c.svcProc.informer.GetIndexer())

	return nil
}

func (c *Controller) processServicesItem(string) error {
	return nil
}
