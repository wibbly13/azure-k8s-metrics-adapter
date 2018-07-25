/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/jsturtevant/azure-k8-metrics-adapter/pkg/aim"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/metrics/pkg/apis/custom_metrics"

	"github.com/kubernetes-incubator/custom-metrics-apiserver/pkg/provider"
	"k8s.io/metrics/pkg/apis/external_metrics"

	"github.com/Azure/azure-sdk-for-go/services/servicebus/mgmt/2017-04-01/servicebus"
	"github.com/Azure/go-autorest/autorest/azure/auth"
)

type externalMetric struct {
	info  provider.ExternalMetricInfo
	value external_metrics.ExternalMetricValue
}

var (
	testingMetrics = []externalMetric{
		{
			info: provider.ExternalMetricInfo{
				Metric: "my-external-metric",
				Labels: map[string]string{
					"foo": "bar",
				},
			},
			value: external_metrics.ExternalMetricValue{
				MetricName: "my-external-metric",
				MetricLabels: map[string]string{
					"foo": "bar",
				},
				Value: *resource.NewQuantity(42, resource.DecimalSI),
			},
		},
		{
			info: provider.ExternalMetricInfo{
				Metric: "my-external-metric",
				Labels: map[string]string{
					"foo": "baz",
				},
			},
			value: external_metrics.ExternalMetricValue{
				MetricName: "my-external-metric",
				MetricLabels: map[string]string{
					"foo": "baz",
				},
				Value: *resource.NewQuantity(43, resource.DecimalSI),
			},
		},
		{
			info: provider.ExternalMetricInfo{
				Metric: "other-external-metric",
				Labels: map[string]string{},
			},
			value: external_metrics.ExternalMetricValue{
				MetricName:   "other-external-metric",
				MetricLabels: map[string]string{},
				Value:        *resource.NewQuantity(44, resource.DecimalSI),
			},
		},
	}
)

type testingProvider struct {
	client dynamic.Interface
	mapper apimeta.RESTMapper

	values          map[provider.CustomMetricInfo]int64
	externalMetrics []externalMetric
}

func NewFakeProvider(client dynamic.Interface, mapper apimeta.RESTMapper) provider.MetricsProvider {
	return &testingProvider{
		client:          client,
		mapper:          mapper,
		values:          make(map[provider.CustomMetricInfo]int64),
		externalMetrics: testingMetrics,
	}
}

func (p *testingProvider) valueFor(groupResource schema.GroupResource, metricName string, namespaced bool) (int64, error) {
	info := provider.CustomMetricInfo{
		GroupResource: groupResource,
		Metric:        metricName,
		Namespaced:    namespaced,
	}

	info, _, err := info.Normalized(p.mapper)
	if err != nil {
		return 0, err
	}

	value := p.values[info]
	value += 1
	p.values[info] = value

	return value, nil
}

func (p *testingProvider) metricFor(value int64, groupResource schema.GroupResource, namespace string, name string, metricName string) (*custom_metrics.MetricValue, error) {
	kind, err := p.mapper.KindFor(groupResource.WithVersion(""))
	if err != nil {
		return nil, err
	}

	return &custom_metrics.MetricValue{
		DescribedObject: custom_metrics.ObjectReference{
			APIVersion: groupResource.Group + "/" + runtime.APIVersionInternal,
			Kind:       kind.Kind,
			Name:       name,
			Namespace:  namespace,
		},
		MetricName: metricName,
		Timestamp:  metav1.Time{time.Now()},
		Value:      *resource.NewMilliQuantity(value*100, resource.DecimalSI),
	}, nil
}

func (p *testingProvider) metricsFor(totalValue int64, groupResource schema.GroupResource, metricName string, list runtime.Object) (*custom_metrics.MetricValueList, error) {
	if !apimeta.IsListType(list) {
		return nil, fmt.Errorf("returned object was not a list")
	}

	res := make([]custom_metrics.MetricValue, 0)

	err := apimeta.EachListItem(list, func(item runtime.Object) error {
		objMeta := item.(metav1.Object)
		value, err := p.metricFor(0, groupResource, objMeta.GetNamespace(), objMeta.GetName(), metricName)
		if err != nil {
			return err
		}
		res = append(res, *value)

		return nil
	})
	if err != nil {
		return nil, err
	}

	for i := range res {
		res[i].Value = *resource.NewMilliQuantity(100*totalValue/int64(len(res)), resource.DecimalSI)
	}

	//return p.metricFor(value, groupResource, "", name, metricName)
	return &custom_metrics.MetricValueList{
		Items: res,
	}, nil
}

func (p *testingProvider) GetRootScopedMetricByName(groupResource schema.GroupResource, name string, metricName string) (*custom_metrics.MetricValue, error) {
	value, err := p.valueFor(groupResource, metricName, false)
	if err != nil {
		return nil, err
	}
	return p.metricFor(value, groupResource, "", name, metricName)
}

func (p *testingProvider) GetRootScopedMetricBySelector(groupResource schema.GroupResource, selector labels.Selector, metricName string) (*custom_metrics.MetricValueList, error) {
	fullReses, err := p.mapper.ResourcesFor(groupResource.WithVersion(""))
	if err != nil || len(fullReses) == 0 {
		glog.Errorf("unable to get prefered GVRs for GR to list matching resource names: %v", err)
		// don't leak implementation details to the user
		return nil, apierr.NewInternalError(fmt.Errorf("unable to list matching resources"))
	}

	totalValue, err := p.valueFor(groupResource, metricName, false)
	if err != nil {
		return nil, err
	}

	matchingObjectsRaw, err := p.client.Resource(fullReses[0]).
		List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	return p.metricsFor(totalValue, groupResource, metricName, matchingObjectsRaw)
}

func (p *testingProvider) GetNamespacedMetricByName(groupResource schema.GroupResource, namespace string, name string, metricName string) (*custom_metrics.MetricValue, error) {
	value, err := p.valueFor(groupResource, metricName, true)
	if err != nil {
		return nil, err
	}
	return p.metricFor(value, groupResource, namespace, name, metricName)
}

func (p *testingProvider) GetNamespacedMetricBySelector(groupResource schema.GroupResource, namespace string, selector labels.Selector, metricName string) (*custom_metrics.MetricValueList, error) {
	fullReses, err := p.mapper.ResourcesFor(groupResource.WithVersion(""))
	if err != nil || len(fullReses) == 0 {
		glog.Errorf("unable to get prefered GVRs for GR to list matching resource names: %v", err)
		// don't leak implementation details to the user
		return nil, apierr.NewInternalError(fmt.Errorf("unable to list matching resources"))
	}

	totalValue, err := p.valueFor(groupResource, metricName, true)
	if err != nil {
		return nil, err
	}

	matchingObjectsRaw, err := p.client.Resource(fullReses[0]).Namespace(namespace).
		List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	return p.metricsFor(totalValue, groupResource, metricName, matchingObjectsRaw)
}

func (p *testingProvider) ListAllMetrics() []provider.CustomMetricInfo {
	// TODO: maybe dynamically generate this?
	return []provider.CustomMetricInfo{
		{
			GroupResource: schema.GroupResource{Group: "", Resource: "pods"},
			Metric:        "packets-per-second",
			Namespaced:    true,
		},
		{
			GroupResource: schema.GroupResource{Group: "", Resource: "services"},
			Metric:        "connections-per-second",
			Namespaced:    true,
		},
		{
			GroupResource: schema.GroupResource{Group: "", Resource: "namespaces"},
			Metric:        "queue-length",
			Namespaced:    false,
		},
	}
}
func (p *testingProvider) GetExternalMetric(namespace string, metricName string, metricSelector labels.Selector) (*external_metrics.ExternalMetricValueList, error) {
	matchingMetrics := []external_metrics.ExternalMetricValue{}
	for _, metric := range p.externalMetrics {
		if metric.info.Metric == metricName &&
			metricSelector.Matches(labels.Set(metric.info.Labels)) {
			metricValue := metric.value
			metricValue.Timestamp = metav1.Now()
			matchingMetrics = append(matchingMetrics, metricValue)
		}
	}
	return &external_metrics.ExternalMetricValueList{
		Items: matchingMetrics,
	}, nil
}

func (p *testingProvider) ListAllExternalMetrics() []provider.ExternalMetricInfo {
	externalMetricsInfo := []provider.ExternalMetricInfo{}

	subID, err := aim.GetAzureSubscription()
	if err != nil {
		glog.Errorf("unable to get azure config: %v", err)
		return externalMetricsInfo
	}

	namespaceClient := servicebus.NewNamespacesClient(subID)

	// create an authorizer from env vars or Azure Managed Service Idenity
	authorizer, err := auth.NewAuthorizerFromEnvironment()
	if err == nil {
		namespaceClient.Authorizer = authorizer
	}

	// TODO iterate over result set
	result, err := namespaceClient.List(context.Background())
	if err != nil {
		glog.Errorf("unable to get service bus namespaces: %v", err)
		return externalMetricsInfo
	}

	for _, namespace := range result.Values() {
		glog.V(2).Infoln("found namespace", *namespace.Name)
	}

	for _, metric := range p.externalMetrics {
		externalMetricsInfo = append(externalMetricsInfo, metric.info)
	}
	return externalMetricsInfo
}