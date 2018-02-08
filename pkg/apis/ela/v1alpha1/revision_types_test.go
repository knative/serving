/*
Copyright 2018 Google, Inc. All rights reserved.
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

const (
	generation int64 = 5
)

func testGeneration(t *testing.T) {
	r := Revision{}
	if a := r.GetGeneration(); a != 0 {
		t.Errorf("empty revision generation should be 0 was: %d", a)
	}

	r.SetGeneration(5)
	if e, a := generation, r.GetGeneration(); e != a {
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

/*
func TestMismatchedConditions(t *testing.T) {
	rs := RevisionStatus{
		Conditions: []RevisionCondition{
			{
				Type:    RevisionConditionBuildComplete,
				Status:  corev1.ConditionTrue,
				Message: "existing",
			},
		},
	}

	rc := &RevisionCondition{
		Type:    RevisionConditionReady,
		Status:  corev1.ConditionTrue,
		Message: "new one",
	}
	// Trying to add ConditionReady or update BuildComplete but screw up
	// the type here, or in the above definition.
	// End result is is that only the new one is added
	rs.SetCondition(RevisionConditionBuildComplete, rc)
	if len(rs.Conditions) != 2 {
		t.Errorf("BUG?: %+v", rs.Conditions)
	}
}
*/

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
	rs.SetCondition(RevisionConditionBuildComplete, rc)
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
