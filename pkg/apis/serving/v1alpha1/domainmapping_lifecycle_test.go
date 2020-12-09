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

package v1alpha1

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	apistest "knative.dev/pkg/apis/testing"
)

func TestDomainMappingDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1.Conditions{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := duck.VerifyType(&DomainMapping{}, test.t)
			if err != nil {
				t.Errorf("VerifyType(DomainMapping, %T) = %v", test.t, err)
			}
		})
	}
}

func TestDomainMappingGetConditionSet(t *testing.T) {
	r := &DomainMapping{}

	if got, want := r.GetConditionSet().GetTopLevelConditionType(), apis.ConditionReady; got != want {
		t.Errorf("GetTopLevelCondition=%v, want=%v", got, want)
	}
}

func TestDomainMappingGetGroupVersionKind(t *testing.T) {
	r := &DomainMapping{}
	want := schema.GroupVersionKind{
		Group:   "serving.knative.dev",
		Version: "v1alpha1",
		Kind:    "DomainMapping",
	}
	if got := r.GetGroupVersionKind(); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}

func TestDomainClaimConditions(t *testing.T) {
	dms := &DomainMappingStatus{}

	dms.InitializeConditions()
	dms.MarkTLSNotEnabled("AutoTLS not yet available for DomainMapping")
	apistest.CheckConditionOngoing(dms, DomainMappingConditionDomainClaimed, t)
	apistest.CheckConditionOngoing(dms, DomainMappingConditionReady, t)

	dms.MarkDomainClaimFailed("rejected")
	apistest.CheckConditionFailed(dms, DomainMappingConditionDomainClaimed, t)
	apistest.CheckConditionFailed(dms, DomainMappingConditionReady, t)

	dms.MarkDomainClaimed()
	apistest.CheckConditionSucceeded(dms, DomainMappingConditionDomainClaimed, t)
	apistest.CheckConditionOngoing(dms, DomainMappingConditionReady, t)

	dms.MarkReferenceResolved()
	dms.PropagateIngressStatus(netv1alpha1.IngressStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   netv1alpha1.IngressConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apistest.CheckConditionSucceeded(dms, DomainMappingConditionReady, t)

	dms.MarkDomainClaimNotOwned()
	apistest.CheckConditionFailed(dms, DomainMappingConditionDomainClaimed, t)
	apistest.CheckConditionFailed(dms, DomainMappingConditionReady, t)
}

func TestReferenceResolvedCondition(t *testing.T) {
	dms := &DomainMappingStatus{}

	dms.InitializeConditions()
	dms.MarkTLSNotEnabled("AutoTLS not yet available for DomainMapping")
	apistest.CheckConditionOngoing(dms, DomainMappingConditionReferenceResolved, t)
	apistest.CheckConditionOngoing(dms, DomainMappingConditionReady, t)

	dms.MarkReferenceNotResolved("can't get no resolution")
	apistest.CheckConditionFailed(dms, DomainMappingConditionReferenceResolved, t)
	apistest.CheckConditionFailed(dms, DomainMappingConditionReady, t)

	dms.MarkReferenceResolved()
	apistest.CheckConditionSucceeded(dms, DomainMappingConditionReferenceResolved, t)
	apistest.CheckConditionOngoing(dms, DomainMappingConditionReady, t)

	dms.MarkDomainClaimed()
	dms.PropagateIngressStatus(netv1alpha1.IngressStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   netv1alpha1.IngressConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apistest.CheckConditionSucceeded(dms, DomainMappingConditionReady, t)

	dms.MarkReferenceNotResolved("still can't get no resolution")
	apistest.CheckConditionFailed(dms, DomainMappingConditionReferenceResolved, t)
	apistest.CheckConditionFailed(dms, DomainMappingConditionReady, t)
}

func TestCertificateNotReady(t *testing.T) {
	dms := &DomainMappingStatus{}

	dms.InitializeConditions()
	dms.MarkCertificateNotReady("cert pending")

	apistest.CheckConditionOngoing(dms, DomainMappingConditionCertificateProvisioned, t)
}

func TestCertificateProvisionFailed(t *testing.T) {
	dms := &DomainMappingStatus{}

	dms.InitializeConditions()
	dms.MarkCertificateProvisionFailed("cert failed")

	apistest.CheckConditionFailed(dms, DomainMappingConditionCertificateProvisioned, t)
}

func TestDomainMappingNotOwnCertificate(t *testing.T) {
	dms := &DomainMappingStatus{}
	dms.InitializeConditions()
	dms.MarkCertificateNotOwned("cert not owned")

	apistest.CheckConditionFailed(dms, DomainMappingConditionCertificateProvisioned, t)
}

func TestDomainMappingAutoTLSNotEnabled(t *testing.T) {
	dms := &DomainMappingStatus{}
	dms.InitializeConditions()
	dms.MarkTLSNotEnabled(AutoTLSNotEnabledMessage)

	apistest.CheckConditionSucceeded(dms, DomainMappingConditionCertificateProvisioned, t)
}

func TestDomainMappingHTTPDowngrade(t *testing.T) {
	dms := &DomainMappingStatus{}
	dms.InitializeConditions()
	dms.MarkHTTPDowngrade("downgraded to HTTP because we can't obtain cert")

	apistest.CheckConditionSucceeded(dms, DomainMappingConditionCertificateProvisioned, t)
}

func TestPropagateIngressStatus(t *testing.T) {
	dms := &DomainMappingStatus{}

	dms.InitializeConditions()
	dms.MarkTLSNotEnabled("AutoTLS not yet available for DomainMapping")
	apistest.CheckConditionOngoing(dms, DomainMappingConditionIngressReady, t)
	apistest.CheckConditionOngoing(dms, DomainMappingConditionReady, t)

	dms.PropagateIngressStatus(netv1alpha1.IngressStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   netv1alpha1.IngressConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	})

	apistest.CheckConditionFailed(dms, DomainMappingConditionIngressReady, t)
	apistest.CheckConditionFailed(dms, DomainMappingConditionReady, t)

	dms.PropagateIngressStatus(netv1alpha1.IngressStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   netv1alpha1.IngressConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})

	apistest.CheckConditionSucceeded(dms, DomainMappingConditionIngressReady, t)
	apistest.CheckConditionOngoing(dms, DomainMappingConditionReady, t)

	dms.MarkDomainClaimed()
	dms.MarkReferenceResolved()
	apistest.CheckConditionSucceeded(dms, DomainMappingConditionReady, t)

	dms.PropagateIngressStatus(netv1alpha1.IngressStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   netv1alpha1.IngressConditionReady,
				Status: corev1.ConditionUnknown,
			}},
		},
	})

	apistest.CheckConditionOngoing(dms, DomainMappingConditionIngressReady, t)
	apistest.CheckConditionOngoing(dms, DomainMappingConditionReady, t)
}

func TestDomainMappingIsReady(t *testing.T) {
	cases := []struct {
		name    string
		status  DomainMappingStatus
		isReady bool
	}{{
		name:    "empty status should not be ready",
		status:  DomainMappingStatus{},
		isReady: false,
	}, {
		name: "Different condition type should not be ready",
		status: DomainMappingStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   DomainMappingConditionIngressReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: false,
	}, {
		name: "False condition status should not be ready",
		status: DomainMappingStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   DomainMappingConditionReady,
					Status: corev1.ConditionFalse,
				}},
			},
		},
		isReady: false,
	}, {
		name: "Unknown condition status should not be ready",
		status: DomainMappingStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   DomainMappingConditionReady,
					Status: corev1.ConditionUnknown,
				}},
			},
		},
		isReady: false,
	}, {
		name: "Missing condition status should not be ready",
		status: DomainMappingStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type: DomainMappingConditionReady,
				}},
			},
		},
		isReady: false,
	}, {
		name: "True condition status should be ready",
		status: DomainMappingStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   DomainMappingConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status should be ready",
		status: DomainMappingStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   DomainMappingConditionIngressReady,
					Status: corev1.ConditionTrue,
				}, {
					Type:   DomainMappingConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status false should not be ready",
		status: DomainMappingStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   DomainMappingConditionIngressReady,
					Status: corev1.ConditionTrue,
				}, {
					Type:   DomainMappingConditionReady,
					Status: corev1.ConditionFalse,
				}},
			},
		},
		isReady: false,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if e, a := tc.isReady, tc.status.IsReady(); e != a {
				t.Errorf("%q expected: %v got: %v", tc.name, e, a)
			}

			dm := &DomainMapping{}
			dm.Status = tc.status
			if e, a := tc.isReady, dm.IsReady(); e != a {
				t.Errorf("%q expected: %v got: %v", tc.name, e, a)
			}

			dm.Generation = 2
			dm.Status.ObservedGeneration = 1
			if dm.IsReady() {
				t.Error("Expected DomainMapping not to be Ready when ObservedGeneration != Generation")
			}
		})
	}
}
