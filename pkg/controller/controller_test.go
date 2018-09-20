package controller

import (
	"testing"

	api "github.com/Azure/azure-k8s-metrics-adapter/pkg/apis/externalmetric/v1alpha1"
	"github.com/Azure/azure-k8s-metrics-adapter/pkg/client/clientset/versioned/fake"
	informers "github.com/Azure/azure-k8s-metrics-adapter/pkg/client/informers/externalversions"
	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

type controllerConfig struct {
	// store is the fake etcd backing store that the go client will
	// use to push to the controller.  Add anything the controller to
	// process to the store
	store          []runtime.Object
	syncedFunction cache.InformerSynced
	enqueuer       func(c *Controller) func(obj interface{})
}

type wanted struct {
	keepRunning  bool
	itemsRemaing int
}

type testConfig struct {
	controllerConfig controllerConfig
	want             wanted
}

func TestProcessRunsToCompletion(t *testing.T) {
	var storeObjects []runtime.Object

	externalMetric := newExternalMetric("test")
	storeObjects = append(storeObjects, externalMetric)

	testConfig := testConfig{
		controllerConfig: controllerConfig{
			store:          storeObjects,
			syncedFunction: alwaysSynced,
		},
		want: wanted{
			itemsRemaing: 0,
			keepRunning:  true,
		},
	}

	runControllerTests(testConfig, t)
}

func TestInvalidItemOnQueue(t *testing.T) {
	var storeObjects []runtime.Object

	externalMetric := newExternalMetric("test")
	storeObjects = append(storeObjects, externalMetric)

	// force the queue to have anything other than a string
	// to exersize the invalid queue path
	var badenquer = func(c *Controller) func(obj interface{}) {
		enquer := func(obj interface{}) {
			var key string

			glog.V(2).Infof("adding item to queue for '%s'", key)
			c.externalMetricqueue.AddRateLimited(obj)
		}

		return enquer
	}

	testConfig := testConfig{
		controllerConfig: controllerConfig{
			store:          storeObjects,
			syncedFunction: alwaysSynced,
			enqueuer:       badenquer,
		},
		want: wanted{
			itemsRemaing: 0,
			keepRunning:  true,
		},
	}

	runControllerTests(testConfig, t)
}

func runControllerTests(testConfig testConfig, t *testing.T) {
	c, i := newController(testConfig.controllerConfig)

	stopCh := make(chan struct{})
	defer close(stopCh)
	i.Start(stopCh)

	keepRunning := c.processNextItem()

	if keepRunning != testConfig.want.keepRunning {
		t.Errorf("c.processNextItem() = %v, want %v", keepRunning, testConfig.want.keepRunning)
	}

	items := c.externalMetricqueue.Len()

	if items != testConfig.want.itemsRemaing {
		t.Errorf("c.processNextItem() = %v, want %v", keepRunning, testConfig.want.itemsRemaing)
	}
}

func newController(config controllerConfig) (*Controller, informers.SharedInformerFactory) {
	fakeClient := fake.NewSimpleClientset(config.store...)
	i := informers.NewSharedInformerFactory(fakeClient, 0)

	c := NewController(i.Azure().V1alpha1().ExternalMetrics(), succesFakeHandler{})
	c.externalMetricSynced = config.syncedFunction

	if config.enqueuer != nil {
		c.enqueuer = config.enqueuer(c)
	}

	return c, i
}

func newExternalMetric(name string) *api.ExternalMetric {
	return &api.ExternalMetric{
		TypeMeta: metav1.TypeMeta{APIVersion: api.SchemeGroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: api.ExternalMetricSpec{
			AzureConfig:  api.AzureConfig{},
			MetricConfig: api.MetricConfig{},
		},
	}
}

type succesFakeHandler struct{}

func (h succesFakeHandler) Process(key string) error {
	return nil
}

var alwaysSynced = func() bool { return true }
