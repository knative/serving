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
	"github.com/knative/serving/pkg/apis/autoscaling"
	pav1alpha1 "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	sv1a1 "github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/autoscaling/kpa/resources/names"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	kpaLabelKey     = autoscaling.GroupName + "/kpa"
	metricsPortName = "metrics"
	// metricsPort is the external port of the service for metrics.
	metricsPort = int32(9090)
)

// makeLabels propagates labels from parent resources and adds a KPA label.
func makeLabels(pa *pav1alpha1.PodAutoscaler) map[string]string {
	labels := make(map[string]string, len(pa.ObjectMeta.Labels)+1)
	for k, v := range pa.ObjectMeta.Labels {
		labels[k] = v
	}
	labels[kpaLabelKey] = pa.Name
	return labels
}

// makeAnnotations propagates annotations from the parent resource.
func makeAnnotations(pa *pav1alpha1.PodAutoscaler) map[string]string {
	annotations := make(map[string]string, len(pa.ObjectMeta.Annotations))
	for k, v := range pa.ObjectMeta.Annotations {
		annotations[k] = v
	}
	return annotations
}

// MakeMetricsService constructs a service that can be scraped for metrics.
func MakeMetricsService(pa *pav1alpha1.PodAutoscaler, selector map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.MetricsServiceName(pa.Name),
			Namespace:       pa.Namespace,
			Labels:          makeLabels(pa),
			Annotations:     makeAnnotations(pa),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(pa)},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:       metricsPortName,
				Protocol:   corev1.ProtocolTCP,
				Port:       metricsPort,
				TargetPort: intstr.FromString(sv1a1.RequestQueueMetricsPortName),
			}},
			Selector: selector,
		},
	}
}
