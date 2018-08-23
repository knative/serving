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
	fakebuildclientset "github.com/knative/build/pkg/client/clientset/versioned/fake"
	fakesharedclientset "github.com/knative/pkg/client/clientset/versioned/fake"
	"github.com/knative/pkg/controller"
	. "github.com/knative/pkg/logging/testing"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	"github.com/knative/serving/pkg/reconciler"
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

type Ctor func(*Listers, reconciler.Options) controller.Reconciler

func (r *TableRow) Test(t *testing.T, ctor Ctor) {
	ls := NewListers(r.Objects)

	kubeClient := fakekubeclientset.NewSimpleClientset(ls.GetKubeObjects()...)
	sharedClient := fakesharedclientset.NewSimpleClientset(ls.GetSharedObjects()...)
	client := fakeclientset.NewSimpleClientset(ls.GetServingObjects()...)
	buildClient := fakebuildclientset.NewSimpleClientset(ls.GetBuildObjects()...)

	// Set up our Controller from the fakes.
	c := ctor(&ls, reconciler.Options{
		KubeClientSet:    kubeClient,
		SharedClientSet:  sharedClient,
		BuildClientSet:   buildClient,
		ServingClientSet: client,
		Logger:           TestLogger(t),
	})

	for _, reactor := range r.WithReactors {
		kubeClient.PrependReactor("*", "*", reactor)
		sharedClient.PrependReactor("*", "*", reactor)
		client.PrependReactor("*", "*", reactor)
		buildClient.PrependReactor("*", "*", reactor)
	}

	// Validate all Create operations through the serving client.
	client.PrependReactor("create", "*", ValidateCreates)
	client.PrependReactor("update", "*", ValidateUpdates)

	// Run the Reconcile we're testing.
	if err := c.Reconcile(context.TODO(), r.Key); (err != nil) != r.WantErr {
		t.Errorf("Reconcile() error = %v, WantErr %v", err, r.WantErr)
	}
	// Now check that the Reconcile had the desired effects.
	expectedNamespace, _, _ := cache.SplitMetaNamespaceKey(r.Key)

	createActions, updateActions, deleteActions, patchActions := extractActions(t, sharedClient, buildClient, client, kubeClient)

	for i, want := range r.WantCreates {
		if i >= len(createActions) {
			t.Errorf("Missing create: %v", want)
			continue
		}
		got := createActions[i]
		if got.GetNamespace() != expectedNamespace {
			t.Errorf("unexpected action[%d]: %#v", i, got)
		}
		obj := got.GetObject()
		if diff := cmp.Diff(want, obj, ignoreLastTransitionTime, safeDeployDiff, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("unexpected create (-want +got): %s", diff)
		}
	}
	if got, want := len(createActions), len(r.WantCreates); got > want {
		for _, extra := range createActions[want:] {
			t.Errorf("Extra create: %v", extra)
		}
	}

	for i, want := range r.WantUpdates {
		if i >= len(updateActions) {
			t.Errorf("Missing update: %v", want.GetObject())
			continue
		}
		got := updateActions[i]
		if diff := cmp.Diff(want.GetObject(), got.GetObject(), ignoreLastTransitionTime, safeDeployDiff, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("unexpected update (-want +got): %s", diff)
		}
	}
	if got, want := len(updateActions), len(r.WantUpdates); got > want {
		for _, extra := range updateActions[want:] {
			t.Errorf("Extra update: %v", extra)
		}
	}

	for i, want := range r.WantDeletes {
		if i >= len(deleteActions) {
			t.Errorf("Missing delete: %v", want)
			continue
		}
		got := deleteActions[i]
		if got.GetName() != want.Name {
			t.Errorf("unexpected delete[%d]: %#v", i, got)
		}
		if got.GetNamespace() != expectedNamespace {
			t.Errorf("unexpected delete[%d]: %#v", i, got)
		}
	}
	if got, want := len(deleteActions), len(r.WantDeletes); got > want {
		for _, extra := range deleteActions[want:] {
			t.Errorf("Extra delete: %v", extra)
		}
	}

	for i, want := range r.WantPatches {
		if i >= len(patchActions) {
			t.Errorf("Missing patch: %v", want)
			continue
		}

		got := patchActions[i]
		if got.GetName() != want.Name {
			t.Errorf("unexpected patch[%d]: %#v", i, got)
		}
		if got.GetNamespace() != expectedNamespace {
			t.Errorf("unexpected patch[%d]: %#v", i, got)
		}
		if diff := cmp.Diff(string(want.GetPatch()), string(got.GetPatch())); diff != "" {
			t.Errorf("unexpected patch(-want +got): %s", diff)
		}
	}
	if got, want := len(patchActions), len(r.WantPatches); got > want {
		for _, extra := range patchActions[want:] {
			t.Errorf("Extra patch: %v", extra)
		}
	}
}

type hasActions interface {
	Actions() []clientgotesting.Action
}

func extractActions(t *testing.T, clients ...hasActions) (
	createActions []clientgotesting.CreateAction,
	updateActions []clientgotesting.UpdateAction,
	deleteActions []clientgotesting.DeleteAction,
	patchActions []clientgotesting.PatchAction,
) {

	for _, c := range clients {
		for _, action := range c.Actions() {
			switch action.GetVerb() {
			case "create":
				createActions = append(createActions,
					action.(clientgotesting.CreateAction))
			case "update":
				updateActions = append(updateActions,
					action.(clientgotesting.UpdateAction))
			case "delete":
				deleteActions = append(deleteActions,
					action.(clientgotesting.DeleteAction))
			case "patch":
				patchActions = append(patchActions,
					action.(clientgotesting.PatchAction))
			default:
				t.Errorf("Unexpected verb %v: %+v", action.GetVerb(), action)
			}
		}
	}
	return
}

type TableTest []TableRow

func (tt TableTest) Test(t *testing.T, ctor Ctor) {
	for _, test := range tt {
		t.Run(test.Name, func(t *testing.T) {
			test.Test(t, ctor)
		})
	}
}

var ignoreLastTransitionTime = cmp.FilterPath(func(p cmp.Path) bool {
	return strings.HasSuffix(p.String(), "LastTransitionTime.Inner.Time")
}, cmp.Ignore())

var safeDeployDiff = cmpopts.IgnoreUnexported(resource.Quantity{})
