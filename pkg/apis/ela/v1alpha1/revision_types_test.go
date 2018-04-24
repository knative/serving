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
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestGeneration(t *testing.T) {
	r := Revision{}
	if a := r.GetGeneration(); a != 0 {
		t.Errorf("empty revision generation should be 0 was: %d", a)
	}

	r.SetGeneration(5)
	if e, a := int64(5), r.GetGeneration(); e != a {
		t.Errorf("getgeneration mismatch expected: %d got: %d", e, a)
	}

}

func TestIsReady(t *testing.T) {
	cases := []struct {
		name    string
		status  RevisionStatus
		isReady bool
	}{
		{
			name:    "empty status should not be ready",
			status:  RevisionStatus{},
			isReady: false,
		},
		{
			name: "Different condition type should not be ready",
			status: RevisionStatus{
				Conditions: []RevisionCondition{
					{
						Type:   RevisionConditionBuildComplete,
						Status: corev1.ConditionTrue,
					},
				},
			},
			isReady: false,
		},
		{
			name: "False condition status should not be ready",
			status: RevisionStatus{
				Conditions: []RevisionCondition{
					{
						Type:   RevisionConditionReady,
						Status: corev1.ConditionFalse,
					},
				},
			},
			isReady: false,
		},
		{
			name: "Unknown condition status should not be ready",
			status: RevisionStatus{
				Conditions: []RevisionCondition{
					{
						Type:   RevisionConditionReady,
						Status: corev1.ConditionUnknown,
					},
				},
			},
			isReady: false,
		},
		{
			name: "Missing condition status should not be ready",
			status: RevisionStatus{
				Conditions: []RevisionCondition{
					{
						Type: RevisionConditionReady,
					},
				},
			},
			isReady: false,
		},
		{
			name: "True condition status should be ready",
			status: RevisionStatus{
				Conditions: []RevisionCondition{
					{
						Type:   RevisionConditionReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
			isReady: true,
		},
		{
			name: "Multiple conditions with ready status should be ready",
			status: RevisionStatus{
				Conditions: []RevisionCondition{
					{
						Type:   RevisionConditionBuildComplete,
						Status: corev1.ConditionTrue,
					},
					{
						Type:   RevisionConditionReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
			isReady: true,
		},
		{
			name: "Multiple conditions with ready status false should not be ready",
			status: RevisionStatus{
				Conditions: []RevisionCondition{
					{
						Type:   RevisionConditionBuildComplete,
						Status: corev1.ConditionTrue,
					},
					{
						Type:   RevisionConditionReady,
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

func TestGetSetCondition(t *testing.T) {
	rs := RevisionStatus{}
	if a := rs.GetCondition(RevisionConditionReady); a != nil {
		t.Errorf("empty RevisionStatus returned %v when expected nil", a)
	}

	rc := &RevisionCondition{
		Type:   RevisionConditionBuildComplete,
		Status: corev1.ConditionTrue,
	}
	// Set Condition and make sure it's the only thing returned
	rs.SetCondition(rc)
	if e, a := rc, rs.GetCondition(RevisionConditionBuildComplete); !reflect.DeepEqual(e, a) {
		t.Errorf("GetCondition expected %v got: %v", e, a)
	}
	if a := rs.GetCondition(RevisionConditionReady); a != nil {
		t.Errorf("GetCondition expected nil got: %v", a)
	}
	// Remove and make sure it's no longer there
	rs.RemoveCondition(RevisionConditionBuildComplete)
	if a := rs.GetCondition(RevisionConditionBuildComplete); a != nil {
		t.Errorf("empty RevisionStatus returned %v when expected nil", a)
	}
}

func TestRevisionConditions(t *testing.T) {
	rev := &Revision{}
	foo := &RevisionCondition{
		Type:   "Foo",
		Status: "True",
	}
	bar := &RevisionCondition{
		Type:   "Bar",
		Status: "True",
	}

	// Add a new condition.
	rev.Status.SetCondition(foo)

	if got, want := len(rev.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Remove a non-existent condition.
	rev.Status.RemoveCondition(bar.Type)

	if got, want := len(rev.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Add a second condition.
	rev.Status.SetCondition(bar)

	if got, want := len(rev.Status.Conditions), 2; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Remove an existing condition.
	rev.Status.RemoveCondition(bar.Type)

	if got, want := len(rev.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Add nil condition.
	rev.Status.SetCondition(nil)

	if got, want := len(rev.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}
}
