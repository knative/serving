/*
Copyright 2019 The Knative Authors

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

package resources

import (
	"github.com/knative/pkg/kmeta"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/serverlessservice/resources/names"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	networkingGroup      = "networking.internal.knative.dev"
	sksLabelKey          = networkingGroup + "/serverlessservice"
	servicePortNameHTTP1 = "http"
	servicePortNameH2C   = "http2"

	// ServicePort is the external port of the service
	servicePort = int32(80)
	// MetricsPortName is the name of the external port of the service for metrics
	metricsPortName = "metrics"
	// MetricsPort is the external port of the service for metrics
	metricsPort = int32(9090)

	// RequestQueuePortName specifies the port name to use for http requests
	// in queue-proxy container.
	requestQueuePortName string = "queue-port"
	// RequestQueueMetricsPortName specifies the port name to use for metrics
	// emitted by queue-proxy.
	requestQueueMetricsPortName = "queue-metrics"
)

// makeLabels propagates labels from parents and sets SKS label as well.
func makeLabels(sks *v1alpha1.ServerlessService) map[string]string {
	labels := make(map[string]string, len(sks.ObjectMeta.Labels)+1)
	for k, v := range sks.ObjectMeta.Labels {
		labels[k] = v
	}
	labels[sksLabelKey] = sks.Name
	return labels
}

// makeAnnotations propagates annotations from the SKS.
func makeAnnotations(sks *v1alpha1.ServerlessService) map[string]string {
	annotations := make(map[string]string, len(sks.ObjectMeta.Annotations))
	for k, v := range sks.ObjectMeta.Annotations {
		annotations[k] = v
	}
	return annotations
}

// MakePublicService constructs a K8s Service that is not backed a selector
// and will be manually reconciled by the SKS controller.
func MakePublicService(sks *v1alpha1.ServerlessService) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.PublicService(sks),
			Namespace:       sks.Namespace,
			Labels:          makeLabels(sks),
			Annotations:     makeAnnotations(sks),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(sks)},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:       servicePortName(sks),
				Protocol:   corev1.ProtocolTCP,
				Port:       servicePort,
				TargetPort: intstr.FromString(requestQueuePortName),
			}, {
				Name:       metricsPortName,
				Protocol:   corev1.ProtocolTCP,
				Port:       metricsPort,
				TargetPort: intstr.FromString(requestQueueMetricsPortName),
			}},
		},
	}
}

// MakePublicEndpoints constructs a K8s Endpoints that is not backed a selector
// and will be manually reconciled by the SKS controller.
func MakePublicEndpoints(sks *v1alpha1.ServerlessService, src *corev1.Endpoints) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.PublicService(sks), // Name of Endpoints must match that of Service.
			Namespace:       sks.Namespace,
			Labels:          makeLabels(sks),
			Annotations:     makeAnnotations(sks),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(sks)},
		},
		Subsets: func() (ret []corev1.EndpointSubset) {
			for _, r := range src.Subsets {
				ret = append(ret, *r.DeepCopy())
			}
			return
		}(),
	}
}

// MakePrivateService constructs a K8s service, that is backed by the pod selector
// matching pods created by the revision.
func MakePrivateService(sks *v1alpha1.ServerlessService) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.PrivateService(sks),
			Namespace:       sks.Namespace,
			Labels:          makeLabels(sks),
			Annotations:     makeAnnotations(sks),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(sks)},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:       servicePortName(sks),
				Protocol:   corev1.ProtocolTCP,
				Port:       servicePort,
				TargetPort: intstr.FromString(requestQueuePortName),
			}, {
				Name:       metricsPortName,
				Protocol:   corev1.ProtocolTCP,
				Port:       metricsPort,
				TargetPort: intstr.FromString(requestQueueMetricsPortName),
			}},
			Selector: sks.Spec.Selector,
		},
	}
}

// servicePortName returns the well-known port name.
func servicePortName(sks *v1alpha1.ServerlessService) string {
	if sks.Spec.ProtocolType == "h2c" {
		return servicePortNameH2C
	}
	return servicePortNameHTTP1
}
