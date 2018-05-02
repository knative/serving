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
	"github.com/elafros/elafros/pkg/apis/ela"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/controller"

	corev1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var httpServicePortName = "http"
var servicePort = 80

// MakeRevisionK8sService creates a Service that targets all pods with the same
// ela.RevisionLabelKey label. Traffic is routed to queue-proxy port.
func MakeRevisionK8sService(u *v1alpha1.Revision, ns string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:        controller.GetElaK8SServiceNameForRevision(u),
			Namespace:   ns,
			Labels:      MakeElaResourceLabels(u),
			Annotations: MakeElaResourceAnnotations(u),
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       httpServicePortName,
					Port:       int32(servicePort),
					TargetPort: intstr.IntOrString{Type: intstr.String, StrVal: RequestQueuePortName},
				},
			},
			Type: "NodePort",
			Selector: map[string]string{
				ela.RevisionLabelKey: u.Name,
			},
		},
	}
}
