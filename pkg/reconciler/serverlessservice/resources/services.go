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
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/serverlessservice/resources/names"
	"github.com/knative/serving/pkg/resources"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// targetPort chooses the target (pod) port for the public and private service.
func targetPort(sks *v1alpha1.ServerlessService) intstr.IntOrString {
	if sks.Spec.ProtocolType == networking.ProtocolH2C {
		return intstr.FromInt(networking.BackendHTTP2Port)
	}
	return intstr.FromInt(networking.BackendHTTPPort)
}

// MakePublicService constructs a K8s Service that is not backed a selector
// and will be manually reconciled by the SKS controller.
func MakePublicService(sks *v1alpha1.ServerlessService) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.PublicService(sks.Name),
			Namespace: sks.Namespace,
			Labels: resources.UnionMaps(sks.GetLabels(), map[string]string{
				// Add our own special key.
				networking.SKSLabelKey:    sks.Name,
				networking.ServiceTypeKey: string(networking.ServiceTypePublic),
			}),
			Annotations:     resources.CopyMap(sks.GetAnnotations()),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(sks)},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:       networking.ServicePortName(sks.Spec.ProtocolType),
				Protocol:   corev1.ProtocolTCP,
				Port:       int32(networking.ServicePort(sks.Spec.ProtocolType)),
				TargetPort: targetPort(sks),
			}},
		},
	}
}

// MakePublicEndpoints constructs a K8s Endpoints that is not backed a selector
// and will be manually reconciled by the SKS controller.
func MakePublicEndpoints(sks *v1alpha1.ServerlessService, src *corev1.Endpoints) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.PublicService(sks.Name), // Name of Endpoints must match that of Service.
			Namespace: sks.Namespace,
			Labels: resources.UnionMaps(sks.GetLabels(), map[string]string{
				// Add our own special key.
				networking.SKSLabelKey:    sks.Name,
				networking.ServiceTypeKey: string(networking.ServiceTypePublic),
			}),
			Annotations:     resources.CopyMap(sks.GetAnnotations()),
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
func MakePrivateService(sks *v1alpha1.ServerlessService, selector map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.PrivateService(sks.Name),
			Namespace: sks.Namespace,
			Labels: resources.UnionMaps(sks.GetLabels(), map[string]string{
				// Add our own special key.
				networking.SKSLabelKey:    sks.Name,
				networking.ServiceTypeKey: string(networking.ServiceTypePrivate),
			}),
			Annotations:     resources.CopyMap(sks.GetAnnotations()),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(sks)},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:     networking.ServicePortName(sks.Spec.ProtocolType),
				Protocol: corev1.ProtocolTCP,
				// TODO(vagababov): make this work with matching port.
				Port: networking.ServiceHTTPPort,
				// This one is matching the public one, since this is the
				// port queue-proxy listens on.
				TargetPort: targetPort(sks),
			}},
			Selector: selector,
		},
	}
}
