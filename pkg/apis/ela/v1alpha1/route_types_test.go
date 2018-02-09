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
)

func TestRouteConditions(t *testing.T) {
	svc := &Route{}
	foo := &RouteCondition{
		Type:   "Foo",
		Status: "True",
	}
	bar := &RouteCondition{
		Type:   "Bar",
		Status: "True",
	}

	// Add a new condition.
	svc.Status.SetCondition(foo)

	if got, want := len(svc.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Remove a non-existent condition.
	svc.Status.RemoveCondition(bar.Type)

	if got, want := len(svc.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Add a second condition.
	svc.Status.SetCondition(bar)

	if got, want := len(svc.Status.Conditions), 2; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Remove an existing condition.
	svc.Status.RemoveCondition(bar.Type)

	if got, want := len(svc.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}

	// Add nil condition.
	svc.Status.SetCondition(nil)

	if got, want := len(svc.Status.Conditions), 1; got != want {
		t.Fatalf("Unexpected Condition length; got %d, want %d", got, want)
	}
}
