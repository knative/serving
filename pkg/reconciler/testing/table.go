/*
Copyright 2018 The Knative Authors.

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

package testing

import (
	"context"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/knative/pkg/controller"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
)

// TableRow holds a single row of our table test.
type TableRow struct {
	// Name is a descriptive name for this test suitable as a first argument to t.Run()
	Name string

	// Objects holds the state of the world at the onset of reconciliation.
	Objects []runtime.Object

	// Key is the parameter to reconciliation.
	// This has the form "namespace/name".
	Key string

	// WantErr holds whether we should expect the reconciliation to result in an error.
	WantErr bool

	// WantCreates holds the set of Create calls we expect during reconciliation.
	WantCreates []metav1.Object

	// WantUpdates holds the set of Update calls we expect during reconciliation.
	WantUpdates []clientgotesting.UpdateActionImpl

	// WantDeletes holds the set of Delete calls we expect during reconciliation.
	WantDeletes []clientgotesting.DeleteActionImpl

	// WantPatches holds the set of Patch calls we expect during reconcilliation
	WantPatches []clientgotesting.PatchActionImpl

	// WithReactors is a set of functions that are installed as Reactors for the execution
	// of this row of the table-driven-test.
	WithReactors []clientgotesting.ReactionFunc
}

type Factory func(*testing.T, *TableRow) (controller.Reconciler, ActionRecorderList)

func (r *TableRow) Test(t *testing.T, factory Factory) {
	c, recorderList := factory(t, r)

	// Run the Reconcile we're testing.
	if err := c.Reconcile(context.TODO(), r.Key); (err != nil) != r.WantErr {
		t.Errorf("Reconcile() error = %v, WantErr %v", err, r.WantErr)
	}

	expectedNamespace, _, _ := cache.SplitMetaNamespaceKey(r.Key)

	actions, err := recorderList.ActionsByVerb()

	if err != nil {
		t.Errorf("Error capturing actions by verb: %q", err)
	}

	for i, want := range r.WantCreates {
		if i >= len(actions.Creates) {
			t.Errorf("Missing create: %#v", want)
			continue
		}
		got := actions.Creates[i]
		if got.GetNamespace() != expectedNamespace {
			t.Errorf("unexpected action[%d]: %#v", i, got)
		}
		obj := got.GetObject()
		if diff := cmp.Diff(want, obj, ignoreLastTransitionTime, safeDeployDiff, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("unexpected create (-want +got): %s", diff)
		}
	}
	if got, want := len(actions.Creates), len(r.WantCreates); got > want {
		for _, extra := range actions.Creates[want:] {
			t.Errorf("Extra create: %#v", extra)
		}
	}

	for i, want := range r.WantUpdates {
		if i >= len(actions.Updates) {
			t.Errorf("Missing update: %#v", want.GetObject())
			continue
		}
		got := actions.Updates[i]
		if diff := cmp.Diff(want.GetObject(), got.GetObject(), ignoreLastTransitionTime, safeDeployDiff, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("unexpected update (-want +got): %s", diff)
		}
	}
	if got, want := len(actions.Updates), len(r.WantUpdates); got > want {
		for _, extra := range actions.Updates[want:] {
			t.Errorf("Extra update: %#v", extra)
		}
	}

	for i, want := range r.WantDeletes {
		if i >= len(actions.Deletes) {
			t.Errorf("Missing delete: %#v", want)
			continue
		}
		got := actions.Deletes[i]
		if got.GetName() != want.GetName() {
			t.Errorf("unexpected delete[%d]: %#v", i, got)
		}
		if got.GetNamespace() != expectedNamespace {
			t.Errorf("unexpected delete[%d]: %#v", i, got)
		}
	}
	if got, want := len(actions.Deletes), len(r.WantDeletes); got > want {
		for _, extra := range actions.Deletes[want:] {
			t.Errorf("Extra delete: %#v", extra)
		}
	}

	for i, want := range r.WantPatches {
		if i >= len(actions.Patches) {
			t.Errorf("Missing patch: %#v", want)
			continue
		}

		got := actions.Patches[i]
		if got.GetName() != want.GetName() {
			t.Errorf("unexpected patch[%d]: %#v", i, got)
		}
		if got.GetNamespace() != expectedNamespace {
			t.Errorf("unexpected patch[%d]: %#v", i, got)
		}
		if diff := cmp.Diff(string(want.GetPatch()), string(got.GetPatch())); diff != "" {
			t.Errorf("unexpected patch(-want +got): %s", diff)
		}
	}
	if got, want := len(actions.Patches), len(r.WantPatches); got > want {
		for _, extra := range actions.Patches[want:] {
			t.Errorf("Extra patch: %#v", extra)
		}
	}
}

type TableTest []TableRow

func (tt TableTest) Test(t *testing.T, factory Factory) {
	for _, test := range tt {
		t.Run(test.Name, func(t *testing.T) {
			test.Test(t, factory)
		})
	}
}

var ignoreLastTransitionTime = cmp.FilterPath(func(p cmp.Path) bool {
	return strings.HasSuffix(p.String(), "LastTransitionTime.Inner.Time")
}, cmp.Ignore())

var safeDeployDiff = cmpopts.IgnoreUnexported(resource.Quantity{})
