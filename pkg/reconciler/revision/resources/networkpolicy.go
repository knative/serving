/*
Copyright 2020 The Knative Authors

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
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/pkg/kmeta"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/revision/resources/names"
)

// Creates a Network Policy
func MakeNetworkPolicy(rev *v1.Revision) *netv1.NetworkPolicy {
	return &netv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.NetworkPolicy(rev),
			Namespace:       rev.Namespace,
			Labels:          makeLabels(rev),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(rev)},
		},
		Spec: netv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "serving.knative.dev/revision",
						Operator: metav1.LabelSelectorOpExists,
					},
				},
			},
			Ingress: []netv1.NetworkPolicyIngressRule{
				{
					Ports: []netv1.NetworkPolicyPort{{
						Port: &intstr.IntOrString{
							Type:   intstr.String,
							StrVal: "queue-port",
						},
					}},
					From: []netv1.NetworkPolicyPeer{},
				},
			},
			Egress:      []netv1.NetworkPolicyEgressRule{},
			PolicyTypes: []netv1.PolicyType{"Ingress"},
		},
	}
}
