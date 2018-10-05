/*
Copyright 2018 The Knative Authors

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
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestGeneration(t *testing.T) {
	r := ClusterIngress{}
	if a := r.GetGeneration(); a != 0 {
		t.Errorf("empty ClusterIngress generation should be 0 was: %d", a)
	}

	r.SetGeneration(5)
	if e, a := int64(5), r.GetGeneration(); e != a {
		t.Errorf("getgeneration mismatch expected: %d got: %d", e, a)
	}

}

func TestTypicalFlow(t *testing.T) {
	r := &ClusterIngress{}
	r.Status.InitializeConditions()

	checkConditionOngoingClusterIngress(r.Status, ClusterIngressConditionReady, t)

	// Then network is configured.
	r.Status.MarkNetworkConfigured()
	checkConditionSucceededClusterIngress(r.Status, ClusterIngressConditionNetworkConfigured, t)
	checkConditionOngoingClusterIngress(r.Status, ClusterIngressConditionReady, t)

	// Then ingress has address.
	r.Status.MarkLoadBalancerReady([]LoadBalancerIngress{{DomainInternal: "gateway.default.svc"}})
	checkConditionSucceededClusterIngress(r.Status, ClusterIngressConditionLoadBalancerReady, t)
	checkConditionSucceededClusterIngress(r.Status, ClusterIngressConditionReady, t)
	checkIsReady(r.Status, t)
}

func TestGetSpecJSON(t *testing.T) {
	ci := ClusterIngress{
		Spec: IngressSpec{
			TLS: []ClusterIngressTLS{{
				SecretNamespace: "secret-space",
				SecretName:      "secret-name",
			}},
			Rules: []ClusterIngressRule{{
				Hosts: []string{"example.com"},
				ClusterIngressRuleValue: ClusterIngressRuleValue{
					HTTP: &HTTPClusterIngressRuleValue{
						Paths: []HTTPClusterIngressPath{{
							Splits: []ClusterIngressBackendSplit{{
								Backend: &ClusterIngressBackend{
									ServiceName:      "revision-000",
									ServiceNamespace: "default",
									ServicePort:      intstr.FromInt(8080),
								},
							}},
							Retries: &HTTPRetry{
								Attempts: 3,
							},
						}},
					},
				},
			}},
		},
	}
	bytes, err := ci.GetSpecJSON()
	if err != nil {
		t.Fatalf("Saw %v, wanted: nil", err)
	}
	cis := IngressSpec{}
	json.Unmarshal(bytes, &cis)
	if diff := cmp.Diff(ci.Spec, cis); diff != "" {
		t.Errorf("Unexpected diff (-want, +got) = %v", diff)
	}
}

func TestGetGroupVersionKind(t *testing.T) {
	ci := ClusterIngress{}
	expected := SchemeGroupVersion.WithKind("ClusterIngress")
	if diff := cmp.Diff(expected, ci.GetGroupVersionKind()); diff != "" {
		t.Errorf("Unexpected diff (-want, +got) = %v", diff)
	}
}

func checkIsReady(cis IngressStatus, t *testing.T) {
	t.Helper()
	if !cis.IsReady() {
		t.Fatal("IsReady()=false, wanted true")
	}
}

func checkConditionSucceededClusterIngress(cis IngressStatus, c duckv1alpha1.ConditionType, t *testing.T) *duckv1alpha1.Condition {
	t.Helper()
	return checkConditionClusterIngress(cis, c, corev1.ConditionTrue, t)
}

func checkConditionFailedClusterIngress(cis IngressStatus, c duckv1alpha1.ConditionType, t *testing.T) *duckv1alpha1.Condition {
	t.Helper()
	return checkConditionClusterIngress(cis, c, corev1.ConditionFalse, t)
}

func checkConditionOngoingClusterIngress(cis IngressStatus, c duckv1alpha1.ConditionType, t *testing.T) *duckv1alpha1.Condition {
	t.Helper()
	return checkConditionClusterIngress(cis, c, corev1.ConditionUnknown, t)
}

func checkConditionClusterIngress(cis IngressStatus, c duckv1alpha1.ConditionType, cs corev1.ConditionStatus, t *testing.T) *duckv1alpha1.Condition {
	t.Helper()
	cond := cis.GetCondition(c)
	if cond == nil {
		t.Fatalf("Get(%v) = nil, wanted %v=%v", c, c, cs)
	}
	if cond.Status != cs {
		t.Fatalf("Get(%v) = %v, wanted %v", c, cond.Status, cs)
	}
	return cond
}
