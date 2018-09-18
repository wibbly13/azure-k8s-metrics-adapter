package controller

import (
	"fmt"

	listers "github.com/Azure/azure-k8s-metrics-adapter/pkg/client/listers/externalmetric/v1alpha1"
	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
)

type Handler struct {
	externalmetricLister listers.ExternalMetricLister
}

func NewHandler(externalmetricLister listers.ExternalMetricLister) Handler {
	return Handler{
		externalmetricLister: externalmetricLister,
	}
}

func (handler *Handler) Process(namespace string, name string) error {
	glog.V(2).Infof("processing item '%s' in namespace '%s'", namespace, name)

	// Get the Foo resource with this namespace/name
	externalMetricInfo, err := handler.externalmetricLister.ExternalMetrics(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("external metric '%s' in namespace '%s' from work queue no longer exists", name, namespace))
			return nil
		}

		return err
	}

	glog.V(2).Infof("externalmetric name: %s", externalMetricInfo.Spec.MetricConfig.MetricName)
	glog.V(2).Infof("externalmetric filter: %s", externalMetricInfo.Spec.MetricConfig.Filter)
	glog.V(2).Infof("externalmetric aggregation: %s", externalMetricInfo.Spec.MetricConfig.Aggregation)
	glog.V(2).Infof("externalmetric rg: %s", externalMetricInfo.Spec.AzureConfig.ResourceGroup)
	glog.V(2).Infof("externalmetric resourcename: %s", externalMetricInfo.Spec.AzureConfig.ResourceName)
	glog.V(2).Infof("externalmetric rp namespace: %s", externalMetricInfo.Spec.AzureConfig.ResourceProviderNamespace)
	glog.V(2).Infof("externalmetric resource type: %s", externalMetricInfo.Spec.AzureConfig.ResourceType)
	glog.V(2).Infof("externalmetric sub: %s", externalMetricInfo.Spec.AzureConfig.SubscriptionID)

	return nil
}
