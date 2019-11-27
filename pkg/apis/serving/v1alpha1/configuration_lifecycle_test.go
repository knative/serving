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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	apitestv1 "knative.dev/pkg/apis/testing/v1"
)

func TestConfigurationDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1.Conditions{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := duck.VerifyType(&Configuration{}, test.t)
			if err != nil {
				t.Errorf("VerifyType(Configuration, %T) = %v", test.t, err)
			}
		})
	}
}

func TestConfigurationIsReady(t *testing.T) {
	cases := []struct {
		name    string
		status  ConfigurationStatus
		isReady bool
	}{{
		name:    "empty status should not be ready",
		status:  ConfigurationStatus{},
		isReady: false,
	}, {
		name: "Different condition type should not be ready",
		status: ConfigurationStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   "Foo",
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: false,
	}, {
		name: "False condition status should not be ready",
		status: ConfigurationStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   ConfigurationConditionReady,
					Status: corev1.ConditionFalse,
				}},
			},
		},
		isReady: false,
	}, {
		name: "Unknown condition status should not be ready",
		status: ConfigurationStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   ConfigurationConditionReady,
					Status: corev1.ConditionUnknown,
				}},
			},
		},
		isReady: false,
	}, {
		name: "Missing condition status should not be ready",
		status: ConfigurationStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type: ConfigurationConditionReady,
				}},
			},
		},
		isReady: false,
	}, {
		name: "True condition status should be ready",
		status: ConfigurationStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   ConfigurationConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status should be ready",
		status: ConfigurationStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   "Foo",
					Status: corev1.ConditionTrue,
				}, {
					Type:   ConfigurationConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status false should not be ready",
		status: ConfigurationStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   "Foo",
					Status: corev1.ConditionTrue,
				}, {
					Type:   ConfigurationConditionReady,
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

func TestLatestReadyRevisionNameUpToDate(t *testing.T) {
	cases := []struct {
		name           string
		status         ConfigurationStatus
		isUpdateToDate bool
	}{{
		name: "Not ready status should not be up-to-date",
		status: ConfigurationStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   ConfigurationConditionReady,
					Status: corev1.ConditionFalse,
				}},
			},
		},
		isUpdateToDate: false,
	}, {
		name: "Missing LatestReadyRevisionName should not be up-to-date",
		status: ConfigurationStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   ConfigurationConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
			ConfigurationStatusFields: ConfigurationStatusFields{
				LatestCreatedRevisionName: "rev-1",
			},
		},
		isUpdateToDate: false,
	}, {
		name: "Different revision names should not be up-to-date",
		status: ConfigurationStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   ConfigurationConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
			ConfigurationStatusFields: ConfigurationStatusFields{
				LatestCreatedRevisionName: "rev-2",
				LatestReadyRevisionName:   "rev-1",
			},
		},
		isUpdateToDate: false,
	}, {
		name: "Same revision names and ready status should be up-to-date",
		status: ConfigurationStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   ConfigurationConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
			ConfigurationStatusFields: ConfigurationStatusFields{
				LatestCreatedRevisionName: "rev-1",
				LatestReadyRevisionName:   "rev-1",
			},
		},
		isUpdateToDate: true,
	}}

	for _, tc := range cases {
		if e, a := tc.isUpdateToDate, tc.status.IsLatestReadyRevisionNameUpToDate(); e != a {
			t.Errorf("%q expected: %v got: %v", tc.name, e, a)
		}
	}
}

func TestTypicalFlow(t *testing.T) {
	r := &ConfigurationStatus{}
	r.InitializeConditions()
	apitestv1.CheckConditionOngoing(r.duck(), ConfigurationConditionReady, t)
	r.SetLatestCreatedRevisionName("foo")
	apitestv1.CheckConditionOngoing(r.duck(), ConfigurationConditionReady, t)

	r.SetLatestReadyRevisionName("foo")
	apitestv1.CheckConditionSucceeded(r.duck(), ConfigurationConditionReady, t)

	// Verify a second call to SetLatestCreatedRevisionName doesn't change the status from Ready
	// e.g. on a subsequent reconciliation.
	r.SetLatestCreatedRevisionName("foo")
	apitestv1.CheckConditionSucceeded(r.duck(), ConfigurationConditionReady, t)

	r.SetLatestCreatedRevisionName("bar")
	apitestv1.CheckConditionOngoing(r.duck(), ConfigurationConditionReady, t)

	r.SetLatestReadyRevisionName("bar")
	apitestv1.CheckConditionSucceeded(r.duck(), ConfigurationConditionReady, t)

	r.MarkResourceNotConvertible(ConvertErrorf("build", "something something not allowed.").(*CannotConvertError))
	apitestv1.CheckConditionSucceeded(r.duck(), ConfigurationConditionReady, t)
	apitestv1.CheckConditionFailed(r.duck(), ConditionTypeConvertible, t)
}

func TestFailingFirstRevisionWithRecovery(t *testing.T) {
	r := &ConfigurationStatus{}
	r.InitializeConditions()
	apitestv1.CheckConditionOngoing(r.duck(), ConfigurationConditionReady, t)

	// Our first attempt to create the revision fails
	const want = "transient API server failure"
	r.MarkRevisionCreationFailed(want)
	apitestv1.CheckConditionFailed(r.duck(), ConfigurationConditionReady, t)
	if c := r.GetCondition(ConfigurationConditionReady); !strings.Contains(c.Message, want) {
		t.Errorf("MarkRevisionCreationFailed = %v, want substring %v", c.Message, want)
	}

	r.SetLatestCreatedRevisionName("foo")
	apitestv1.CheckConditionOngoing(r.duck(), ConfigurationConditionReady, t)

	// Then we create it, but it fails to come up.
	const want2 = "the message"
	r.MarkLatestCreatedFailed("foo", want2)
	apitestv1.CheckConditionFailed(r.duck(), ConfigurationConditionReady, t)
	if c := r.GetCondition(ConfigurationConditionReady); !strings.Contains(c.Message, want2) {
		t.Errorf("MarkLatestCreatedFailed = %v, want substring %v", c.Message, want2)
	}

	// When a new revision comes along the Ready condition becomes Unknown.
	r.SetLatestCreatedRevisionName("bar")
	apitestv1.CheckConditionOngoing(r.duck(), ConfigurationConditionReady, t)

	// When the new revision becomes ready, then Ready becomes true as well.
	r.SetLatestReadyRevisionName("bar")
	apitestv1.CheckConditionSucceeded(r.duck(), ConfigurationConditionReady, t)
}

func TestFailingSecondRevision(t *testing.T) {
	r := &ConfigurationStatus{}
	r.InitializeConditions()
	apitestv1.CheckConditionOngoing(r.duck(), ConfigurationConditionReady, t)

	r.SetLatestCreatedRevisionName("foo")
	apitestv1.CheckConditionOngoing(r.duck(), ConfigurationConditionReady, t)

	r.SetLatestReadyRevisionName("foo")
	apitestv1.CheckConditionSucceeded(r.duck(), ConfigurationConditionReady, t)

	r.SetLatestCreatedRevisionName("bar")
	apitestv1.CheckConditionOngoing(r.duck(), ConfigurationConditionReady, t)

	// When the second revision fails, the Configuration becomes Failed.
	const want = "the message"
	r.MarkLatestCreatedFailed("bar", want)
	apitestv1.CheckConditionFailed(r.duck(), ConfigurationConditionReady, t)
	if c := r.GetCondition(ConfigurationConditionReady); !strings.Contains(c.Message, want) {
		t.Errorf("MarkLatestCreatedFailed = %v, want substring %v", c.Message, want)
	}
}

func TestLatestRevisionDeletedThenFixed(t *testing.T) {
	r := &ConfigurationStatus{}
	r.InitializeConditions()
	apitestv1.CheckConditionOngoing(r.duck(), ConfigurationConditionReady, t)

	r.SetLatestCreatedRevisionName("foo")
	apitestv1.CheckConditionOngoing(r.duck(), ConfigurationConditionReady, t)

	r.SetLatestReadyRevisionName("foo")
	apitestv1.CheckConditionSucceeded(r.duck(), ConfigurationConditionReady, t)

	// When the latest revision is deleted, the Configuration became Failed.
	const want = "was deleted"
	r.MarkLatestReadyDeleted()
	apitestv1.CheckConditionFailed(r.duck(), ConfigurationConditionReady, t)
	if cnd := r.GetCondition(ConfigurationConditionReady); cnd == nil || !strings.Contains(cnd.Message, want) {
		t.Errorf("MarkLatestReadyDeleted = %v, want substring %v", cnd.Message, want)
	}

	// But creating new revision 'bar' and making it Ready will fix things.
	r.SetLatestCreatedRevisionName("bar")
	apitestv1.CheckConditionOngoing(r.duck(), ConfigurationConditionReady, t)

	r.SetLatestReadyRevisionName("bar")
	apitestv1.CheckConditionSucceeded(r.duck(), ConfigurationConditionReady, t)
}

func TestConfigurationGetGroupVersionKind(t *testing.T) {
	c := &Configuration{}
	want := schema.GroupVersionKind{
		Group:   "serving.knative.dev",
		Version: "v1alpha1",
		Kind:    "Configuration",
	}
	if got := c.GetGroupVersionKind(); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}
