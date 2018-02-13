/*
Copyright 2018 Google LLC. All rights reserved.
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
	config.Status.SetCondition(foo)

	if got, want := len(config.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Remove a non-existent condition.
	config.Status.RemoveCondition(bar.Type)

	if got, want := len(config.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Add a second condition.
	config.Status.SetCondition(bar)

	if got, want := len(config.Status.Conditions), 2; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Remove an existing condition.
	config.Status.RemoveCondition(bar.Type)

	if got, want := len(config.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Add nil condition.
	config.Status.SetCondition(nil)

	if got, want := len(config.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}
}
