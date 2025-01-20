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

package testing

import (
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/autoscaling"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/reconciler/serverlessservice/resources/names"
)

// PodAutoscalerOption is an option that can be applied to a PA.
type PodAutoscalerOption func(*autoscalingv1alpha1.PodAutoscaler)

// WithProtocolType sets the protocol type on the PodAutoscaler.
func WithProtocolType(pt networking.ProtocolType) PodAutoscalerOption {
	return func(pa *autoscalingv1alpha1.PodAutoscaler) {
		pa.Spec.ProtocolType = pt
	}
}

// WithReachability sets the reachability of the PodAutoscaler to the given value.
func WithReachability(r autoscalingv1alpha1.ReachabilityType) PodAutoscalerOption {
	return func(pa *autoscalingv1alpha1.PodAutoscaler) {
		pa.Spec.Reachability = r
	}
}

// WithReachabilityUnknown sets the reachability of the PodAutoscaler to unknown.
func WithReachabilityUnknown(pa *autoscalingv1alpha1.PodAutoscaler) {
	WithReachability(autoscalingv1alpha1.ReachabilityUnknown)(pa)
}

// WithReachabilityReachable sets the reachability of the PodAutoscaler to reachable.
func WithReachabilityReachable(pa *autoscalingv1alpha1.PodAutoscaler) {
	WithReachability(autoscalingv1alpha1.ReachabilityReachable)(pa)
}

// WithReachabilityUnreachable sets the reachability of the PodAutoscaler to unreachable.
func WithReachabilityUnreachable(pa *autoscalingv1alpha1.PodAutoscaler) {
	WithReachability(autoscalingv1alpha1.ReachabilityUnreachable)(pa)
}

// WithPAOwnersRemoved clears the owner references of this PA resource.
func WithPAOwnersRemoved(pa *autoscalingv1alpha1.PodAutoscaler) {
	pa.OwnerReferences = nil
}

// MarkResourceNotOwnedByPA marks PA as not owning a resource it is supposed to own.
func MarkResourceNotOwnedByPA(rType, name string) PodAutoscalerOption {
	return func(pa *autoscalingv1alpha1.PodAutoscaler) {
		pa.Status.MarkResourceNotOwned(rType, name)
	}
}

// WithPodAutoscalerOwnersRemoved clears the owner references of this PodAutoscaler.
func WithPodAutoscalerOwnersRemoved(r *autoscalingv1alpha1.PodAutoscaler) {
	r.OwnerReferences = nil
}

// WithTraffic updates the PA to reflect it receiving traffic.
func WithTraffic(pa *autoscalingv1alpha1.PodAutoscaler) {
	pa.Status.MarkActive()
}

// WithPASKSReady marks PA status that SKS is ready.
func WithPASKSReady(pa *autoscalingv1alpha1.PodAutoscaler) {
	pa.Status.MarkSKSReady()
}

// WithPASKSNotReady marks PA status that SKS is not ready.
func WithPASKSNotReady(m string) PodAutoscalerOption {
	return func(pa *autoscalingv1alpha1.PodAutoscaler) {
		pa.Status.MarkSKSNotReady(m)
	}
}

// WithScaleTargetInitialized updates the PA to reflect it having initialized its
// ScaleTarget.
func WithScaleTargetInitialized(pa *autoscalingv1alpha1.PodAutoscaler) {
	pa.Status.MarkScaleTargetInitialized()
}

// WithPAStatusService annotates PA Status with the provided service name.
func WithPAStatusService(svc string) PodAutoscalerOption {
	return func(pa *autoscalingv1alpha1.PodAutoscaler) {
		pa.Status.ServiceName = svc
	}
}

// WithPAMetricsService annotates PA Status with the provided service name.
func WithPAMetricsService(svc string) PodAutoscalerOption {
	return func(pa *autoscalingv1alpha1.PodAutoscaler) {
		pa.Status.MetricsServiceName = svc
	}
}

// WithBufferedTraffic updates the PA to reflect that it has received
// and buffered traffic while it is being activated.
func WithBufferedTraffic(pa *autoscalingv1alpha1.PodAutoscaler) {
	pa.Status.MarkActivating("Queued",
		"Requests to the target are being buffered as resources are provisioned.")
}

// WithNoTraffic updates the PA to reflect the fact that it is not
// receiving traffic.
func WithNoTraffic(reason, message string) PodAutoscalerOption {
	return func(pa *autoscalingv1alpha1.PodAutoscaler) {
		pa.Status.MarkInactive(reason, message)
	}
}

// WithDeletionTimestamp will set the DeletionTimestamp on the object.
func WithDeletionTimestamp[T metav1.Object](obj T) T {
	t := metav1.NewTime(time.Unix(1e9, 0))
	obj.SetDeletionTimestamp(&t)
	return obj
}

// WithHPAClass updates the PA to add the hpa class annotation.
func WithHPAClass(pa *autoscalingv1alpha1.PodAutoscaler) {
	if pa.Annotations == nil {
		pa.Annotations = make(map[string]string, 1)
	}
	pa.Annotations[autoscaling.ClassAnnotationKey] = autoscaling.HPA
}

// WithPAContainerConcurrency returns a PodAutoscalerOption which sets
// the PodAutoscaler containerConcurrency to the provided value.
func WithPAContainerConcurrency(cc int64) PodAutoscalerOption {
	return func(pa *autoscalingv1alpha1.PodAutoscaler) {
		pa.Spec.ContainerConcurrency = cc
	}
}

func withAnnotationValue(key, value string) PodAutoscalerOption {
	return func(pa *autoscalingv1alpha1.PodAutoscaler) {
		if pa.Annotations == nil {
			pa.Annotations = make(map[string]string, 1)
		}
		pa.Annotations[key] = value
	}
}

// WithTargetAnnotation returns a PodAutoscalerOption which sets
// the PodAutoscaler autoscaling.knative.dev/target annotation to the
// provided value.
func WithTargetAnnotation(target string) PodAutoscalerOption {
	return withAnnotationValue(autoscaling.TargetAnnotationKey, target)
}

// WithTUAnnotation returns a PodAutoscalerOption which sets
// the PodAutoscaler autoscaling.knative.dev/targetUtilizationPercentage
// annotation to the provided value.
func WithTUAnnotation(tu string) PodAutoscalerOption {
	return withAnnotationValue(autoscaling.TargetUtilizationPercentageKey, tu)
}

// WithWindowAnnotation returns a PodAutoScalerOption which sets
// the PodAutoscaler autoscaling.knative.dev/window annotation to the
// provided value.
func WithWindowAnnotation(window string) PodAutoscalerOption {
	return withAnnotationValue(autoscaling.WindowAnnotationKey, window)
}

// WithPanicThresholdPercentageAnnotation returns a PodAutoscalerOption
// which sets the PodAutoscaler
// autoscaling.knative.dev/panicThresholdPercentage annotation to the
// provided value.
func WithPanicThresholdPercentageAnnotation(percentage string) PodAutoscalerOption {
	return withAnnotationValue(autoscaling.PanicThresholdPercentageAnnotationKey, percentage)
}

// WithPanicWindowPercentageAnnotation retturn a PodAutoscalerOption
// which set the PodAutoscaler
// autoscaling.knative.dev/panicWindowPercentage annotation to the
// provided value.
func WithPanicWindowPercentageAnnotation(percentage string) PodAutoscalerOption {
	return withAnnotationValue(autoscaling.PanicWindowPercentageAnnotationKey, percentage)
}

// WithMetricAnnotation adds a metric annotation to the PA.
func WithMetricAnnotation(metric string) PodAutoscalerOption {
	return withAnnotationValue(autoscaling.MetricAnnotationKey, metric)
}

// WithObservedGeneration returns a PodAutoScalerOption which sets
// the Status.ObservedGeneration field to the given generation.
func WithObservedGeneration(gen int64) PodAutoscalerOption {
	return func(pa *autoscalingv1alpha1.PodAutoscaler) {
		pa.Status.ObservedGeneration = gen
	}
}

// WithMetricOwnersRemoved clears the owner references of this PodAutoscaler.
func WithMetricOwnersRemoved(m *autoscalingv1alpha1.Metric) {
	m.OwnerReferences = nil
}

// WithUpperScaleBound sets maxScale to the given number.
func WithUpperScaleBound(i int) PodAutoscalerOption {
	return withAnnotationValue(autoscaling.MaxScaleAnnotationKey, strconv.Itoa(i))
}

// WithLowerScaleBound sets minScale to the given number.
func WithLowerScaleBound(i int) PodAutoscalerOption {
	return withAnnotationValue(autoscaling.MinScaleAnnotationKey, strconv.Itoa(i))
}

// K8sServiceOption enables further configuration of the Kubernetes Service.
type K8sServiceOption func(*corev1.Service)

// OverrideServiceName changes the name of the Kubernetes Service.
func OverrideServiceName(name string) K8sServiceOption {
	return func(svc *corev1.Service) {
		svc.Name = name
	}
}

// MutateK8sService changes the service in a way that must be reconciled.
func MutateK8sService(svc *corev1.Service) {
	// An effective hammer ;-P
	svc.Spec = corev1.ServiceSpec{}
}

// WithClusterIP assigns a ClusterIP to the K8s Service.
func WithClusterIP(ip string) K8sServiceOption {
	return func(svc *corev1.Service) {
		svc.Spec.ClusterIP = ip
	}
}

// WithExternalName gives external name to the K8s Service.
func WithExternalName(name string) K8sServiceOption {
	return func(svc *corev1.Service) {
		svc.Spec.ExternalName = name
		svc.Spec.Ports = []corev1.ServicePort{{
			Name:        networking.ServicePortNameH2C,
			AppProtocol: &networking.AppProtocolH2C,
			Port:        int32(80),
			TargetPort:  intstr.FromInt(80),
		}}
	}
}

// WithK8sSvcOwnersRemoved clears the owner references of this Service.
func WithK8sSvcOwnersRemoved(svc *corev1.Service) {
	svc.OwnerReferences = nil
}

// EndpointsOption enables further configuration of the Kubernetes Endpoints.
type EndpointsOption func(*corev1.Endpoints)

// WithSubsets adds subsets to the body of an Endpoints object.
func WithSubsets(ep *corev1.Endpoints) {
	ep.Subsets = []corev1.EndpointSubset{{
		Addresses: []corev1.EndpointAddress{{IP: "127.0.0.1"}},
		Ports:     []corev1.EndpointPort{{Port: 8012}, {Port: 8013}},
	}}
}

// WithEndpointsOwnersRemoved clears the owner references of this Endpoints resource.
func WithEndpointsOwnersRemoved(eps *corev1.Endpoints) {
	eps.OwnerReferences = nil
}

// PodOption enables further configuration of a Pod.
type PodOption func(*corev1.Pod)

// WithPodCondition sets a condition in the status
func WithPodCondition(conditionType corev1.PodConditionType, status corev1.ConditionStatus, reason string) PodOption {
	return func(pod *corev1.Pod) {
		for i, condition := range pod.Status.Conditions {
			if condition.Type == conditionType {
				pod.Status.Conditions[i].Status = status
				pod.Status.Conditions[i].Reason = reason
				return
			}
		}

		pod.Status.Conditions = append(pod.Status.Conditions, corev1.PodCondition{
			Type:   conditionType,
			Status: status,
			Reason: reason,
		})
	}
}

// WithFailingContainer sets the .Status.ContainerStatuses on the pod to
// include a container named accordingly to fail with the given state.
func WithFailingContainer(name string, exitCode int, message string) PodOption {
	return func(pod *corev1.Pod) {
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
			Name: name,
			LastTerminationState: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{
					ExitCode: int32(exitCode),
					Message:  message,
				},
			},
		}}
	}
}

// WithUnschedulableContainer sets the .Status.Conditions on the pod to
// include `PodScheduled` status to `False` with the given message and reason.
func WithUnschedulableContainer(reason, message string) PodOption {
	return func(pod *corev1.Pod) {
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Reason:  reason,
			Message: message,
			Status:  corev1.ConditionFalse,
		}}
	}
}

// WithWaitingContainer sets the .Status.ContainerStatuses on the pod to
// include a container named accordingly to wait with the given state.
func WithWaitingContainer(name, reason, message string) PodOption {
	return func(pod *corev1.Pod) {
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
			Name: name,
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{
					Reason:  reason,
					Message: message,
				},
			},
		}}
	}
}

// IngressOption enables further configuration of the Ingress.
type IngressOption func(*netv1alpha1.Ingress)

// WithHosts sets the Hosts of the ingress rule specified index
func WithHosts(index int, hosts ...string) IngressOption {
	return func(ingress *netv1alpha1.Ingress) {
		ingress.Spec.Rules[index].Hosts = hosts
	}
}

// WithLoadbalancerFailed marks the respective status as failed using
// the given reason and message.
func WithLoadbalancerFailed(reason, message string) IngressOption {
	return func(ingress *netv1alpha1.Ingress) {
		ingress.Status.MarkLoadBalancerFailed(reason, message)
	}
}

// SKSOption is a callback type for decorate SKS objects.
type SKSOption func(sks *netv1alpha1.ServerlessService)

// WithPubService annotates SKS status with the given service name.
func WithPubService(sks *netv1alpha1.ServerlessService) {
	sks.Status.ServiceName = sks.Name
}

// WithDeployRef annotates SKS with a deployment objectRef.
func WithDeployRef(name string) SKSOption {
	return func(sks *netv1alpha1.ServerlessService) {
		sks.Spec.ObjectRef = corev1.ObjectReference{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       name,
		}
	}
}

// WithSKSReady marks SKS as ready.
func WithSKSReady(sks *netv1alpha1.ServerlessService) {
	WithPrivateService(sks)
	WithPubService(sks)
	sks.Status.MarkEndpointsReady()
}

// WithNumActivators sets the number of requested activators
// on the SKS spec.
func WithNumActivators(n int32) SKSOption {
	return func(sks *netv1alpha1.ServerlessService) {
		sks.Spec.NumActivators = n
	}
}

// WithPrivateService annotates SKS status with the private service name.
func WithPrivateService(sks *netv1alpha1.ServerlessService) {
	sks.Status.PrivateServiceName = names.PrivateService(sks.Name)
}

// WithSKSOwnersRemoved clears the owner references of this SKS resource.
func WithSKSOwnersRemoved(sks *netv1alpha1.ServerlessService) {
	sks.OwnerReferences = nil
}

// WithProxyMode puts SKS into proxy mode.
func WithProxyMode(sks *netv1alpha1.ServerlessService) {
	sks.Spec.Mode = netv1alpha1.SKSOperationModeProxy
}

// SKS creates a generic ServerlessService object.
func SKS(ns, name string, so ...SKSOption) *netv1alpha1.ServerlessService {
	s := &netv1alpha1.ServerlessService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       "test-uid",
		},
		Spec: netv1alpha1.ServerlessServiceSpec{
			Mode:         netv1alpha1.SKSOperationModeServe,
			ProtocolType: networking.ProtocolHTTP1,
			ObjectRef: corev1.ObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "foo-deployment",
			},
		},
	}
	// By default for tests we can presume happy-serve path.
	s.Status.MarkActivatorEndpointsRemoved()
	for _, opt := range so {
		opt(s)
	}
	return s
}
