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
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	duckv1alpha1 "knative.dev/pkg/apis/duck/v1alpha1"
	apitestv1 "knative.dev/pkg/apis/testing/v1"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
)

func TestRouteDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1.Conditions{},
	}, {
		name: "addressable",
		t:    &duckv1alpha1.Addressable{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := duck.VerifyType(&Route{}, test.t)
			if err != nil {
				t.Errorf("VerifyType(Route, %T) = %v", test.t, err)
			}
		})
	}
}

func TestRouteIsReady(t *testing.T) {
	cases := []struct {
		name    string
		status  RouteStatus
		isReady bool
	}{{
		name:    "empty status should not be ready",
		status:  RouteStatus{},
		isReady: false,
	}, {
		name: "Different condition type should not be ready",
		status: RouteStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: false,
	}, {
		name: "False condition status should not be ready",
		status: RouteStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   RouteConditionReady,
					Status: corev1.ConditionFalse,
				}},
			},
		},
		isReady: false,
	}, {
		name: "Unknown condition status should not be ready",
		status: RouteStatus{
			Status: duckv1.Status{

				Conditions: duckv1.Conditions{{
					Type:   RouteConditionReady,
					Status: corev1.ConditionUnknown,
				}},
			},
		},
		isReady: false,
	}, {
		name: "Missing condition status should not be ready",
		status: RouteStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type: RouteConditionReady,
				}},
			},
		},
		isReady: false,
	}, {
		name: "True condition status should be ready",
		status: RouteStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   RouteConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status should be ready",
		status: RouteStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   RouteConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status false should not be ready",
		status: RouteStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   RouteConditionReady,
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
		})
	}
}

func TestTypicalRouteFlow(t *testing.T) {
	r := &RouteStatus{}
	r.InitializeConditions()
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionIngressReady, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionReady, t)

	r.MarkTrafficAssigned()
	apitestv1.CheckConditionSucceeded(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionIngressReady, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionReady, t)

	r.PropagateIngressStatus(netv1alpha1.IngressStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   netv1alpha1.IngressConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apitestv1.CheckConditionSucceeded(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionSucceeded(r.duck(), RouteConditionIngressReady, t)
	apitestv1.CheckConditionSucceeded(r.duck(), RouteConditionReady, t)
}

func TestTrafficNotAssignedFlow(t *testing.T) {
	r := &RouteStatus{}
	r.InitializeConditions()
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionReady, t)

	r.MarkMissingTrafficTarget("Revision", "does-not-exist")
	apitestv1.CheckConditionFailed(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionFailed(r.duck(), RouteConditionReady, t)
}

func TestTargetConfigurationNotYetReadyFlow(t *testing.T) {
	r := &RouteStatus{}
	r.InitializeConditions()
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionReady, t)

	r.MarkConfigurationNotReady("i-have-no-ready-revision")
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionReady, t)
}

func TestUnknownErrorWhenConfiguringTraffic(t *testing.T) {
	r := &RouteStatus{}
	r.InitializeConditions()
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionIngressReady, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionReady, t)

	r.MarkUnknownTrafficError("unknown-error")
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionIngressReady, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionReady, t)
}

func TestTargetConfigurationFailedToBeReadyFlow(t *testing.T) {
	r := &RouteStatus{}
	r.InitializeConditions()
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionIngressReady, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionReady, t)

	r.MarkConfigurationFailed("permanently-failed")
	apitestv1.CheckConditionFailed(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionIngressReady, t)
	apitestv1.CheckConditionFailed(r.duck(), RouteConditionReady, t)
}

func TestTargetRevisionNotYetReadyFlow(t *testing.T) {
	r := &RouteStatus{}
	r.InitializeConditions()
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionIngressReady, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionReady, t)

	r.MarkRevisionNotReady("not-yet-ready")
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionIngressReady, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionReady, t)
}

func TestTargetRevisionFailedToBeReadyFlow(t *testing.T) {
	r := &RouteStatus{}
	r.InitializeConditions()
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionIngressReady, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionReady, t)

	r.MarkRevisionFailed("cannot-find-image")
	apitestv1.CheckConditionFailed(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionIngressReady, t)
	apitestv1.CheckConditionFailed(r.duck(), RouteConditionReady, t)
}

func TestIngressFailureRecovery(t *testing.T) {
	r := &RouteStatus{}
	r.InitializeConditions()
	r.PropagateIngressStatus(netv1alpha1.IngressStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   netv1alpha1.IngressConditionReady,
				Status: corev1.ConditionUnknown,
			}},
		},
	})
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionIngressReady, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionReady, t)

	// Empty IngressStatus marks ingress "NotConfigured"
	r.PropagateIngressStatus(netv1alpha1.IngressStatus{})
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionIngressReady, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionReady, t)

	r.MarkTrafficAssigned()
	r.PropagateIngressStatus(netv1alpha1.IngressStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   netv1alpha1.IngressConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apitestv1.CheckConditionSucceeded(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionSucceeded(r.duck(), RouteConditionIngressReady, t)
	apitestv1.CheckConditionSucceeded(r.duck(), RouteConditionReady, t)

	r.PropagateIngressStatus(netv1alpha1.IngressStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   netv1alpha1.IngressConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	})
	apitestv1.CheckConditionSucceeded(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionFailed(r.duck(), RouteConditionIngressReady, t)
	apitestv1.CheckConditionFailed(r.duck(), RouteConditionReady, t)

	r.PropagateIngressStatus(netv1alpha1.IngressStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   netv1alpha1.IngressConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	})
	apitestv1.CheckConditionSucceeded(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionSucceeded(r.duck(), RouteConditionIngressReady, t)
	apitestv1.CheckConditionSucceeded(r.duck(), RouteConditionReady, t)
}

func TestRouteNotOwnedStuff(t *testing.T) {
	r := &RouteStatus{}
	r.InitializeConditions()
	r.PropagateIngressStatus(netv1alpha1.IngressStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   netv1alpha1.IngressConditionReady,
				Status: corev1.ConditionUnknown,
			}},
		},
	})

	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionIngressReady, t)
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionReady, t)

	r.MarkServiceNotOwned("evan")
	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionAllTrafficAssigned, t)
	apitestv1.CheckConditionFailed(r.duck(), RouteConditionIngressReady, t)
	apitestv1.CheckConditionFailed(r.duck(), RouteConditionReady, t)
}

func TestRouteGetGroupVersionKind(t *testing.T) {
	r := &Route{}
	want := schema.GroupVersionKind{
		Group:   "serving.knative.dev",
		Version: "v1alpha1",
		Kind:    "Route",
	}
	if got := r.GetGroupVersionKind(); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}

func TestCertificateReady(t *testing.T) {
	r := &RouteStatus{}
	r.InitializeConditions()
	r.MarkCertificateReady("cert")

	apitestv1.CheckConditionSucceeded(r.duck(), RouteConditionCertificateProvisioned, t)
}

func TestCertificateNotReady(t *testing.T) {
	r := &RouteStatus{}
	r.InitializeConditions()
	r.MarkCertificateNotReady("cert")

	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionCertificateProvisioned, t)
}

func TestCertificateProvisionFailed(t *testing.T) {
	r := &RouteStatus{}
	r.InitializeConditions()
	r.MarkCertificateProvisionFailed("cert")

	apitestv1.CheckConditionFailed(r.duck(), RouteConditionCertificateProvisioned, t)
}

func TestRouteNotOwnCertificate(t *testing.T) {
	r := &RouteStatus{}
	r.InitializeConditions()
	r.MarkCertificateNotOwned("cert")

	apitestv1.CheckConditionFailed(r.duck(), RouteConditionCertificateProvisioned, t)
}

func TestIngressNotConfigured(t *testing.T) {
	r := &RouteStatus{}
	r.InitializeConditions()
	r.MarkIngressNotConfigured()

	apitestv1.CheckConditionOngoing(r.duck(), RouteConditionIngressReady, t)
}
