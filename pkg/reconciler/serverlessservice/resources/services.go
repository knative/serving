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
	pkgnet "knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/kmeta"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/reconciler/serverlessservice/resources/names"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// targetPort chooses the target (pod) port for the public and private service.
func targetPort(sks *v1alpha1.ServerlessService) intstr.IntOrString {
	if sks.Spec.ProtocolType == pkgnet.ProtocolH2C {
		return intstr.FromInt(networking.BackendHTTP2Port)
	}
	return intstr.FromInt(networking.BackendHTTPPort)
}

// MakePublicService constructs a K8s Service that is not backed a selector
// and will be manually reconciled by the SKS controller.
func MakePublicService(sks *v1alpha1.ServerlessService) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sks.Name,
			Namespace: sks.Namespace,
			Labels: kmeta.UnionMaps(sks.GetLabels(), map[string]string{
				// Add our own special key.
				networking.SKSLabelKey:    sks.Name,
				networking.ServiceTypeKey: string(networking.ServiceTypePublic),
			}),
			Annotations:     kmeta.CopyMap(sks.GetAnnotations()),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(sks)},
		},
		Spec: corev1.ServiceSpec{
			Ports: makePublicServicePorts(sks),
		},
	}
}

func makePublicServicePorts(sks *v1alpha1.ServerlessService) []corev1.ServicePort {
	ports := []corev1.ServicePort{{
		Name:       pkgnet.ServicePortName(sks.Spec.ProtocolType),
		Protocol:   corev1.ProtocolTCP,
		Port:       int32(pkgnet.ServicePort(sks.Spec.ProtocolType)),
		TargetPort: targetPort(sks),
	}, {
		// The HTTPS port is used when activator-ca is enabled.
		// Although it is not used by default, we put it here as it should be harmless
		// and makes the code simple.
		Name:       pkgnet.ServicePortNameHTTPS,
		Protocol:   corev1.ProtocolTCP,
		Port:       pkgnet.ServiceHTTPSPort,
		TargetPort: intstr.FromInt(networking.BackendHTTPSPort),
	}}
	return ports
}

// MakePublicEndpoints constructs a K8s Endpoints that is not backed a selector
// and will be manually reconciled by the SKS controller.
func MakePublicEndpoints(sks *v1alpha1.ServerlessService, src *corev1.Endpoints) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sks.Name, // Name of Endpoints must match that of Service.
			Namespace: sks.Namespace,
			Labels: kmeta.UnionMaps(sks.GetLabels(), map[string]string{
				// Add our own special key.
				networking.SKSLabelKey:    sks.Name,
				networking.ServiceTypeKey: string(networking.ServiceTypePublic),
			}),
			Annotations:     kmeta.CopyMap(sks.GetAnnotations()),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(sks)},
		},
		Subsets: FilterSubsetPorts(sks, src.Subsets),
	}
}

// FilterSubsetPorts makes a copy of the ep.Subsets, filtering out ports
// that are not serving (e.g. 8012 for HTTP).
func FilterSubsetPorts(sks *v1alpha1.ServerlessService, subsets []corev1.EndpointSubset) []corev1.EndpointSubset {
	targetPort := targetPort(sks).IntVal
	return filterSubsetPorts(targetPort, subsets)
}

// filterSubsetPorts internal implementation that takes in port.
// Those are not arbitrary endpoints, but the endpoints we construct ourselves,
// thus we know that at least one of the ports will always match.
func filterSubsetPorts(targetPort int32, subsets []corev1.EndpointSubset) []corev1.EndpointSubset {
	if len(subsets) == 0 {
		return nil
	}
	ret := make([]corev1.EndpointSubset, len(subsets))
	for i, sss := range subsets {
		sst := sss
		sst.Ports = nil
		// Find the port we care about and remove all others.
		for j, p := range sss.Ports {
			switch p.Port {
			case networking.BackendHTTPSPort:
				fallthrough
			case targetPort:
				sst.Ports = append(sst.Ports, sss.Ports[j])
			}
		}
		ret[i] = sst
	}
	return ret
}

// MakePrivateService constructs a K8s service, that is backed by the pod selector
// matching pods created by the revision.
func MakePrivateService(sks *v1alpha1.ServerlessService, selector map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.PrivateService(sks.Name),
			Namespace: sks.Namespace,
			Labels: kmeta.UnionMaps(sks.GetLabels(), map[string]string{
				// Add our own special key.
				networking.SKSLabelKey:    sks.Name,
				networking.ServiceTypeKey: string(networking.ServiceTypePrivate),
			}),
			Annotations:     kmeta.CopyMap(sks.GetAnnotations()),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(sks)},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:     pkgnet.ServicePortName(sks.Spec.ProtocolType),
				Protocol: corev1.ProtocolTCP,
				Port:     pkgnet.ServiceHTTPPort,
				// This one is matching the public one, since this is the
				// port queue-proxy listens on.
				TargetPort: targetPort(sks),
			}, {
				Name:       pkgnet.ServicePortNameHTTPS,
				Protocol:   corev1.ProtocolTCP,
				Port:       pkgnet.ServiceHTTPSPort,
				TargetPort: intstr.FromInt(networking.BackendHTTPSPort),
			}, {
				Name:       servingv1.AutoscalingQueueMetricsPortName,
				Protocol:   corev1.ProtocolTCP,
				Port:       networking.AutoscalingQueueMetricsPort,
				TargetPort: intstr.FromString(servingv1.AutoscalingQueueMetricsPortName),
			}, {
				Name:       servingv1.UserQueueMetricsPortName,
				Protocol:   corev1.ProtocolTCP,
				Port:       networking.UserQueueMetricsPort,
				TargetPort: intstr.FromString(servingv1.UserQueueMetricsPortName),
			}, {
				// When run with the Istio mesh, Envoy blocks traffic to any ports not
				// recognized, and has special treatment for probes, but not PreStop hooks.
				// That results in the PreStop hook /wait-for-drain in queue-proxy not
				// reachable, thus triggering SIGTERM immediately during shutdown and
				// causing requests to be dropped.
				//
				// So we expose this port here to work around this Istio bug.
				Name:       servingv1.QueueAdminPortName,
				Protocol:   corev1.ProtocolTCP,
				Port:       networking.QueueAdminPort,
				TargetPort: intstr.FromInt(networking.QueueAdminPort),
			}, {
				// When run with the Istio mesh and with the pod-addressability feature
				// enabled, this mirrors the target port to the "outer" service port to
				// instruct Istio to open the respective listener on the pod.
				Name:       pkgnet.ServicePortName(sks.Spec.ProtocolType) + "-istio",
				Protocol:   corev1.ProtocolTCP,
				Port:       targetPort(sks).IntVal,
				TargetPort: targetPort(sks),
			}},
			Selector: selector,
		},
	}
}
