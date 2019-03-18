/*
Copyright 2019 The Knative Authors.

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

package v1alpha1

import (
	"fmt"

	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var clusterIngressCondSet = duckv1alpha1.NewLivingConditionSet(
	ClusterIngressConditionNetworkConfigured,
	ClusterIngressConditionLoadBalancerReady,
)

func (ci *ClusterIngress) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("ClusterIngress")
}

// IsPublic returns whether the ClusterIngress should be exposed publicly.
func (ci *ClusterIngress) IsPublic() bool {
	return ci.Spec.Visibility == "" || ci.Spec.Visibility == IngressVisibilityExternalIP
}

// GetConditions returns the Conditions array. This enables generic handling of
// conditions by implementing the duckv1alpha1.Conditions interface.
func (cis *IngressStatus) GetConditions() duckv1alpha1.Conditions {
	return cis.Conditions
}

// SetConditions sets the Conditions array. This enables generic handling of
// conditions by implementing the duckv1alpha1.Conditions interface.
func (cis *IngressStatus) SetConditions(conditions duckv1alpha1.Conditions) {
	cis.Conditions = conditions
}

func (cis *IngressStatus) GetCondition(t duckv1alpha1.ConditionType) *duckv1alpha1.Condition {
	return clusterIngressCondSet.Manage(cis).GetCondition(t)
}

func (cis *IngressStatus) InitializeConditions() {
	clusterIngressCondSet.Manage(cis).InitializeConditions()
}

func (cis *IngressStatus) MarkNetworkConfigured() {
	clusterIngressCondSet.Manage(cis).MarkTrue(ClusterIngressConditionNetworkConfigured)
}

// MarkResourceNotOwned changes the "NetworkConfigured" condition to false to reflect that the
// resource of the given kind and name has already been created, and we do not own it.
func (cis *IngressStatus) MarkResourceNotOwned(kind, name string) {
	clusterIngressCondSet.Manage(cis).MarkFalse(ClusterIngressConditionNetworkConfigured, "NotOwned",
		fmt.Sprintf("There is an existing %s %q that we do not own.", kind, name))
}

// MarkLoadBalancerReady marks the Ingress with ClusterIngressConditionLoadBalancerReady,
// and also populate the address of the load balancer.
func (cis *IngressStatus) MarkLoadBalancerReady(lbs []LoadBalancerIngressStatus) {
	cis.LoadBalancer = &LoadBalancerStatus{
		Ingress: []LoadBalancerIngressStatus{},
	}
	cis.LoadBalancer.Ingress = append(cis.LoadBalancer.Ingress, lbs...)
	clusterIngressCondSet.Manage(cis).MarkTrue(ClusterIngressConditionLoadBalancerReady)
}

// IsReady looks at the conditions and if the Status has a condition
// ClusterIngressConditionReady returns true if ConditionStatus is True
func (cis *IngressStatus) IsReady() bool {
	return clusterIngressCondSet.Manage(cis).IsHappy()
}
