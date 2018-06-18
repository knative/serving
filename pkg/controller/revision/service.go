/*
Copyright 2018 Google LLC

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

package revision

import (
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/queue"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var httpServicePortName = "http"
var servicePort = 80

// MakeRevisionK8sService creates a Service that targets all pods with the same
// serving.RevisionLabelKey label. Traffic is routed to queue-proxy port.
func MakeRevisionK8sService(rev *v1alpha1.Revision) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            controller.GetElaK8SServiceNameForRevision(rev),
			Namespace:       controller.GetElaNamespaceName(rev.Namespace),
			Labels:          MakeElaResourceLabels(rev),
			Annotations:     MakeElaResourceAnnotations(rev),
			OwnerReferences: []metav1.OwnerReference{*controller.NewRevisionControllerRef(rev)},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       httpServicePortName,
					Port:       int32(servicePort),
					TargetPort: intstr.IntOrString{Type: intstr.String, StrVal: queue.RequestQueuePortName},
				},
			},
			Type: "NodePort",
			Selector: map[string]string{
				serving.RevisionLabelKey: rev.Name,
			},
		},
	}
}
