package controller

import (
	"errors"
	"testing"

	api "github.com/Azure/azure-k8s-metrics-adapter/pkg/apis/externalmetric/v1alpha1"
	"github.com/Azure/azure-k8s-metrics-adapter/pkg/client/clientset/versioned/fake"
	informers "github.com/Azure/azure-k8s-metrics-adapter/pkg/client/informers/externalversions"
	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type controllerConfig struct {
	// store is the fake etcd backing store that the go client will
	// use to push to the controller.  Add anything the controller to
	// process to the store
	store          []runtime.Object
	syncedFunction cache.InformerSynced
	enqueuer       func(c *Controller) func(obj interface{})
	handler        ContollerHandler
}

type wanted struct {
	keepRunning  bool
	itemsRemaing int

	// number of times added two queue
	// will be zero if the item was forgeten
	enqueCount int
}

func testStore() []runtime.Object {
	var storeObjects []runtime.Object

	externalMetric := newExternalMetric()
	storeObjects = append(storeObjects, externalMetric)
	return storeObjects
}

func TestProcessRunsToCompletion(t *testing.T) {
	tests := []struct {
		name             string
		controllerConfig controllerConfig
		want             wanted
	}{
		{
			name: "test run to completion",
			controllerConfig: controllerConfig{
				store:          testStore(),
				syncedFunction: alwaysSynced,
				handler:        succesFakeHandler{},
			},
			want: wanted{
				itemsRemaing: 0,
				keepRunning:  true,
			},
		},
		{
			name: "failed hanlder reqenques",
			controllerConfig: controllerConfig{
				store:          testStore(),
				syncedFunction: alwaysSynced,
				handler:        failedFakeHandler{},
			},
			want: wanted{
				itemsRemaing: 1,
				keepRunning:  true,
				enqueCount:   2, // should be two because it got added two second time on failure
			},
		},
		{
			name: "invalid item on queue",
			controllerConfig: controllerConfig{
				store:          testStore(),
				syncedFunction: alwaysSynced,
				enqueuer:       badenquer,
				handler:        succesFakeHandler{},
			},
			want: wanted{
				itemsRemaing: 0,
				keepRunning:  true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			runControllerTests(tt.controllerConfig, tt.want, t)
		})
	}
}

func runControllerTests(controllerConfig controllerConfig, want wanted, t *testing.T) {
	c, i := newController(controllerConfig)

	stopCh := make(chan struct{})
	defer close(stopCh)
	i.Start(stopCh)

	keepRunning := c.processNextItem()

	if keepRunning != want.keepRunning {
		t.Errorf("should continue processing = %v, want %v", keepRunning, want.keepRunning)
	}

	items := c.externalMetricqueue.Len()

	if items != want.itemsRemaing {
		t.Errorf("Items still on queue = %v, want %v", items, want.itemsRemaing)
	}

	retrys := c.externalMetricqueue.NumRequeues("default/test")
	if retrys != want.enqueCount {
		t.Errorf("Items enqueued times = %v, want %v", retrys, want.enqueCount)
	}
}

func newController(config controllerConfig) (*Controller, informers.SharedInformerFactory) {
	fakeClient := fake.NewSimpleClientset(config.store...)
	i := informers.NewSharedInformerFactory(fakeClient, 0)

	c := NewController(i.Azure().V1alpha1().ExternalMetrics(), config.handler)

	// override for testing
	c.externalMetricSynced = config.syncedFunction

	if config.enqueuer != nil {
		// override for testings
		c.enqueuer = config.enqueuer(c)
	}

	// override so the item gets added right away for testing with no delay
	c.externalMetricqueue = workqueue.NewNamedRateLimitingQueue(NoDelyRateLimiter(), "nodelay")

	return c, i
}

var badenquer = func(c *Controller) func(obj interface{}) {
	enquer := func(obj interface{}) {
		var key string

		glog.V(2).Infof("adding item to queue for '%s'", key)
		c.externalMetricqueue.AddRateLimited(obj)
	}

	return enquer
}

func newExternalMetric() *api.ExternalMetric {
	return &api.ExternalMetric{
		TypeMeta: metav1.TypeMeta{APIVersion: api.SchemeGroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
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

type failedFakeHandler struct{}

func (h failedFakeHandler) Process(key string) error {
	return errors.New("this fake always fails")
}

var alwaysSynced = func() bool { return true }

func NoDelyRateLimiter() workqueue.RateLimiter {
	return workqueue.NewItemExponentialFailureRateLimiter(0, 0)
}
