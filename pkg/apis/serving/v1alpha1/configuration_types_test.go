/*
Copyright 2018 The Knative Authors. All rights reserved.
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
)

func TestConfigurationGeneration(t *testing.T) {
	config := Configuration{}
	if e, a := int64(0), config.GetGeneration(); e != a {
		t.Errorf("empty revision generation should be 0 was: %d", a)
	}

	config.SetGeneration(5)
	if e, a := int64(5), config.GetGeneration(); e != a {
		t.Errorf("getgeneration mismatch expected: %d got: %d", e, a)
	}
}

func TestConfigurationIsReady(t *testing.T) {
	cases := []struct {
		name    string
		status  ConfigurationStatus
		isReady bool
	}{
		{
			name:    "empty status should not be ready",
			status:  ConfigurationStatus{},
			isReady: false,
		},
		{
			name: "Different condition type should not be ready",
			status: ConfigurationStatus{
				Conditions: []ConfigurationCondition{
					{
						Type:   "Foo",
						Status: corev1.ConditionTrue,
					},
				},
			},
			isReady: false,
		},
		{
			name: "False condition status should not be ready",
			status: ConfigurationStatus{
				Conditions: []ConfigurationCondition{
					{
						Type:   ConfigurationConditionReady,
						Status: corev1.ConditionFalse,
					},
				},
			},
			isReady: false,
		},
		{
			name: "Unknown condition status should not be ready",
			status: ConfigurationStatus{
				Conditions: []ConfigurationCondition{
					{
						Type:   ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					},
				},
			},
			isReady: false,
		},
		{
			name: "Missing condition status should not be ready",
			status: ConfigurationStatus{
				Conditions: []ConfigurationCondition{
					{
						Type: ConfigurationConditionReady,
					},
				},
			},
			isReady: false,
		},
		{
			name: "True condition status should be ready",
			status: ConfigurationStatus{
				Conditions: []ConfigurationCondition{
					{
						Type:   ConfigurationConditionReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
			isReady: true,
		},
		{
			name: "Multiple conditions with ready status should be ready",
			status: ConfigurationStatus{
				Conditions: []ConfigurationCondition{
					{
						Type:   "Foo",
						Status: corev1.ConditionTrue,
					},
					{
						Type:   ConfigurationConditionReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
			isReady: true,
		},
		{
			name: "Multiple conditions with ready status false should not be ready",
			status: ConfigurationStatus{
				Conditions: []ConfigurationCondition{
					{
						Type:   "Foo",
						Status: corev1.ConditionTrue,
					},
					{
						Type:   ConfigurationConditionReady,
						Status: corev1.ConditionFalse,
					},
				},
			},
			isReady: false,
		},
	}

	for _, tc := range cases {
		if e, a := tc.isReady, tc.status.IsReady(); e != a {
			t.Errorf("%q expected: %v got: %v", tc.name, e, a)
		}
	}
}

func TestConfigurationConditions(t *testing.T) {
	config := &Configuration{}
	foo := &ConfigurationCondition{
		Type:   "Foo",
		Status: "True",
	}
	bar := &ConfigurationCondition{
		Type:   "Bar",
		Status: "True",
	}

	// Add a new condition.
	config.Status.setCondition(foo)

	if got, want := len(config.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Remove a non-existent condition.
	config.Status.RemoveCondition(bar.Type)

	if got, want := len(config.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Add a second condition.
	config.Status.setCondition(bar)

	if got, want := len(config.Status.Conditions), 2; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Remove an existing condition.
	config.Status.RemoveCondition(bar.Type)

	if got, want := len(config.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Add nil condition.
	config.Status.setCondition(nil)

	if got, want := len(config.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}
}

func TestLatestReadyRevisionNameUpToDate(t *testing.T) {
	cases := []struct {
		name           string
		status         ConfigurationStatus
		isUpdateToDate bool
	}{
		{
			name: "Not ready status should not be up-to-date",
			status: ConfigurationStatus{
				Conditions: []ConfigurationCondition{
					{
						Type:   ConfigurationConditionReady,
						Status: corev1.ConditionFalse,
					},
				},
			},
			isUpdateToDate: false,
		},
		{
			name: "Missing LatestReadyRevisionName should not be up-to-date",
			status: ConfigurationStatus{
				Conditions: []ConfigurationCondition{
					{
						Type:   ConfigurationConditionReady,
						Status: corev1.ConditionTrue,
					},
				},
				LatestCreatedRevisionName: "rev-1",
			},
			isUpdateToDate: false,
		},
		{
			name: "Different revision names should not be up-to-date",
			status: ConfigurationStatus{
				Conditions: []ConfigurationCondition{
					{
						Type:   ConfigurationConditionReady,
						Status: corev1.ConditionTrue,
					},
				},
				LatestCreatedRevisionName: "rev-2",
				LatestReadyRevisionName:   "rev-1",
			},
			isUpdateToDate: false,
		},
		{
			name: "Same revision names and ready status should be up-to-date",
			status: ConfigurationStatus{
				Conditions: []ConfigurationCondition{
					{
						Type:   ConfigurationConditionReady,
						Status: corev1.ConditionTrue,
					},
				},
				LatestCreatedRevisionName: "rev-1",
				LatestReadyRevisionName:   "rev-1",
			},
			isUpdateToDate: true,
		},
	}

	for _, tc := range cases {
		if e, a := tc.isUpdateToDate, tc.status.IsLatestReadyRevisionNameUpToDate(); e != a {
			t.Errorf("%q expected: %v got: %v", tc.name, e, a)
		}
	}
}

func TestTypicalFlow(t *testing.T) {
	r := &Configuration{}
	r.Status.InitializeConditions()
	checkConditionOngoingConfiguration(r.Status, ConfigurationConditionReady, t)

	r.Status.SetLatestCreatedRevisionName("foo")
	checkConditionOngoingConfiguration(r.Status, ConfigurationConditionReady, t)

	r.Status.SetLatestReadyRevisionName("foo")
	checkConditionSucceededConfiguration(r.Status, ConfigurationConditionReady, t)

	// Verify a second call to SetLatestCreatedRevisionName doesn't change the status from Ready
	// e.g. on a subsequent reconciliation.
	r.Status.SetLatestCreatedRevisionName("foo")
	checkConditionSucceededConfiguration(r.Status, ConfigurationConditionReady, t)

	r.Status.SetLatestCreatedRevisionName("bar")
	checkConditionOngoingConfiguration(r.Status, ConfigurationConditionReady, t)

	r.Status.SetLatestReadyRevisionName("bar")
	checkConditionSucceededConfiguration(r.Status, ConfigurationConditionReady, t)
}

func TestFailingFirstRevisionWithRecovery(t *testing.T) {
	r := &Configuration{}
	r.Status.InitializeConditions()
	checkConditionOngoingConfiguration(r.Status, ConfigurationConditionReady, t)

	r.Status.SetLatestCreatedRevisionName("foo")
	checkConditionOngoingConfiguration(r.Status, ConfigurationConditionReady, t)

	want := "the message"
	r.Status.MarkLatestCreatedFailed("foo", want)
	if c := checkConditionFailedConfiguration(r.Status, ConfigurationConditionReady, t); !strings.Contains(c.Message, want) {
		t.Errorf("MarkLatestCreatedFailed = %v, want substring %v", c.Message, want)
	}

	// When a new revision comes along the Ready condition becomes Unknown.
	r.Status.SetLatestCreatedRevisionName("bar")
	checkConditionOngoingConfiguration(r.Status, ConfigurationConditionReady, t)

	// When the new revision becomes ready, then Ready becomes true as well.
	r.Status.SetLatestReadyRevisionName("bar")
	checkConditionSucceededConfiguration(r.Status, ConfigurationConditionReady, t)
}

func TestFailingSecondRevision(t *testing.T) {
	r := &Configuration{}
	r.Status.InitializeConditions()
	checkConditionOngoingConfiguration(r.Status, ConfigurationConditionReady, t)

	r.Status.SetLatestCreatedRevisionName("foo")
	checkConditionOngoingConfiguration(r.Status, ConfigurationConditionReady, t)

	r.Status.SetLatestReadyRevisionName("foo")
	checkConditionSucceededConfiguration(r.Status, ConfigurationConditionReady, t)

	r.Status.SetLatestCreatedRevisionName("bar")
	checkConditionOngoingConfiguration(r.Status, ConfigurationConditionReady, t)

	// When the second revision fails, the Configuration becomes Failed.
	want := "the message"
	r.Status.MarkLatestCreatedFailed("bar", want)
	if c := checkConditionFailedConfiguration(r.Status, ConfigurationConditionReady, t); !strings.Contains(c.Message, want) {
		t.Errorf("MarkLatestCreatedFailed = %v, want substring %v", c.Message, want)
	}
}

func checkConditionSucceededConfiguration(rs ConfigurationStatus, rct ConfigurationConditionType, t *testing.T) *ConfigurationCondition {
	t.Helper()
	return checkConditionConfiguration(rs, rct, corev1.ConditionTrue, t)
}

func checkConditionFailedConfiguration(rs ConfigurationStatus, rct ConfigurationConditionType, t *testing.T) *ConfigurationCondition {
	t.Helper()
	return checkConditionConfiguration(rs, rct, corev1.ConditionFalse, t)
}

func checkConditionOngoingConfiguration(rs ConfigurationStatus, rct ConfigurationConditionType, t *testing.T) *ConfigurationCondition {
	t.Helper()
	return checkConditionConfiguration(rs, rct, corev1.ConditionUnknown, t)
}

func checkConditionConfiguration(rs ConfigurationStatus, rct ConfigurationConditionType, cs corev1.ConditionStatus, t *testing.T) *ConfigurationCondition {
	t.Helper()
	r := rs.GetCondition(rct)
	if r == nil {
		t.Fatalf("Get(%v) = nil, wanted %v=%v", rct, rct, cs)
	}
	if r.Status != cs {
		t.Fatalf("Get(%v) = %v, wanted %v", rct, r.Status, cs)
	}
	return r
}
