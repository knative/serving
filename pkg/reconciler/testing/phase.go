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
	"fmt"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/reconciler"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	. "github.com/knative/pkg/logging/testing"
)

type (
	// We add these aliases for better readability
	Failures   []clientgotesting.ReactionFunc
	Creates    []runtime.Object
	Updates    []runtime.Object
	Patches    []clientgotesting.PatchActionImpl
	Objects    []runtime.Object
	PhaseTests []PhaseTest

	// PhaseSetupFunc is responsible for creating a phase and setting up
	// any various clients and informers with the given runtime objects.
	//
	// The function should return the list of fake clientsets
	// so the test can assert on create, update and patch actions.
	//
	// PhaseTests will also prepend validation and failure reactors.
	// These failure reactors can be set on the PhaseTests's Failures
	// property
	PhaseSetupFunc func(reconciler.CommonOptions, []runtime.Object) (interface{}, []FakeClient)

	// Resource defines the Kubernetes resource being reconciled
	Resource interface {
		runtime.Object
		metav1.Object
	}

	// FakeClient is used to capture creates, updates, deletes as well as
	// inducing failures
	FakeClient interface {
		ActionRecorder
		PrependReactor(verb, resource string, reaction clientgotesting.ReactionFunc)
	}

	// PhaseTest is used to test a single invocation of a phase's reconcilation
	PhaseTest struct {
		Name string

		// World State
		Resource Resource
		Failures Failures
		Objects  Objects
		Context  context.Context

		// Expectations
		ExpectedCreates Creates
		ExpectedPatches Patches
		ExpectedUpdates Updates
		ExpectError     bool

		// ExpectStatus should be a non-pointer struct that should be
		// compared to the reconciled resource's status
		ExpectedStatus interface{}
	}
)

// Run will iterate over each PhaseTests and invoke them as a subtest
//
// The Kubernetes resource in the phase's reconcile method should match
// the PhaseTest's resource type
func (tests PhaseTests) Run(t *testing.T, setup PhaseSetupFunc) {
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Run(t, setup)
		})
	}
}

// Run will setup the PhaseTest, trigger a reconcile and perform
// the necessary test assertions.
//
// The Kubernetes resource in the phase's reconcile method should match
// the PhaseTest's resource type
func (s *PhaseTest) Run(t *testing.T, setup PhaseSetupFunc) {
	opts := reconciler.CommonOptions{
		Logger:           TestLogger(t),
		Recorder:         &record.FakeRecorder{},
		ObjectTracker:    &NullTracker{},
		ConfigMapWatcher: &FakeConfigMapWatcher{},
		WorkQueue:        &FakeWorkQueue{},
	}

	phase, fakeClients := setup(opts, s.Objects)

	clients := setupClientValidations(fakeClients, s.Failures)
	s.ensurePhaseType(t, phase)

	status, err := s.invokeReconcile(t, phase)

	if (err != nil) != s.ExpectError {
		t.Errorf("Reconcile() error = %v, expected error %v", err, s.ExpectError)
	}

	if diff := cmp.Diff(s.ExpectedStatus, status, cmpOpts); diff != "" {
		t.Errorf("resource status (-want, +got) %s", diff)
	}

	actions, err := clients.ActionsByVerb()

	if err != nil {
		t.Errorf("error capturing actions by verb: %q", err)
	}

	expectedNamespace := s.Resource.GetNamespace()

	assertCreates(t, s.ExpectedCreates, actions.Creates, expectedNamespace)
	assertUpdates(t, s.ExpectedUpdates, actions.Updates, expectedNamespace)
	assertPatches(t, s.ExpectedPatches, actions.Patches, expectedNamespace)
}

func (s *PhaseTest) resourceStatus(resource interface{}) interface{} {
	// TODO(dprotaso) Consider adding `Status()` method to all our resource types
	return reflect.ValueOf(resource).Elem().FieldByName("Status").Interface()
}

func (s *PhaseTest) invokeReconcile(t *testing.T, phase interface{}) (interface{}, error) {
	phaseVal := reflect.ValueOf(phase)
	ctx := context.TODO()

	if s.Context != nil {
		ctx = s.Context
	}

	ctx = logging.WithLogger(ctx, TestLogger(t))

	input := []reflect.Value{
		reflect.ValueOf(ctx),
		reflect.ValueOf(s.Resource),
	}

	output := phaseVal.MethodByName("Reconcile").Call(input)

	var err error = nil

	if !output[1].IsNil() {
		err = output[1].Interface().(error)
	}

	return output[0].Interface(), err
}

func (s *PhaseTest) ensurePhaseType(t *testing.T, phase interface{}) {
	phaseType := reflect.TypeOf(phase)

	if phaseType.Kind() != reflect.Ptr {
		t.Fatalf("expected the constructed phase to be a pointer type %q", phaseType)
	}

	resourceType := reflect.TypeOf(s.Resource)
	statusField, ok := resourceType.Elem().FieldByName("Status")

	if !ok {
		t.Fatalf("Resource %q must have a 'Status' field", resourceType)
	}

	method, ok := phaseType.MethodByName("Reconcile")

	errMsg := fmt.Sprintf(
		"phase should have the method Reconcile(context.Context, %s) (%s, error)",
		resourceType,
		statusField.Type,
	)

	if !ok {
		t.Fatal(errMsg)
	}

	// 0 - index is the receiver
	if method.Type.NumIn() != 3 {
		t.Fatal(errMsg)
	}

	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()

	if method.Type.In(1) != contextType {
		t.Fatal(errMsg)
	}

	if method.Type.In(2) != resourceType {
		t.Fatal(errMsg)
	}

	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if method.Type.NumOut() != 2 {
		t.Fatal(errMsg)
	}

	if method.Type.Out(0) != statusField.Type {
		t.Fatal(errMsg)
	}

	if method.Type.Out(1) != errorType {
		t.Fatal(errMsg)
	}
}

func assertCreates(
	t *testing.T,
	expectedCreates Creates,
	creates []clientgotesting.CreateAction,
	expectedNamespace string,
) {

	for i, want := range expectedCreates {
		if i >= len(creates) {
			t.Errorf("missing create: %v", want)
			continue
		}

		got := creates[i]
		obj := got.GetObject()

		if got.GetNamespace() != expectedNamespace {
			t.Errorf("unexpected action[%d]: %#v", i, got.GetObject())
		}

		if diff := cmp.Diff(want, obj, cmpOpts); diff != "" {
			t.Errorf("unexpected create diff (-want +got): %s", diff)
		}
	}

	if got, want := len(creates), len(expectedCreates); got > want {
		for _, extra := range creates[want:] {
			t.Errorf("extra create actions: %v", extra.GetObject())
		}
	}
}

func assertUpdates(
	t *testing.T,
	expectedUpdates Updates,
	updates []clientgotesting.UpdateAction,
	expectedNamespace string,
) {

	for i, want := range expectedUpdates {
		if i >= len(updates) {
			t.Errorf("missing update: %v", want)
			continue
		}

		got := updates[i]
		obj := got.GetObject()

		if got.GetNamespace() != expectedNamespace {
			t.Errorf("unexpected update action[%d]: %#v", i, got.GetObject())
		}

		if diff := cmp.Diff(want, obj, cmpOpts); diff != "" {
			t.Errorf("unexpected update diff (-want +got): %s", diff)
		}
	}

	if got, want := len(updates), len(expectedUpdates); got > want {
		for _, extra := range updates[want:] {
			t.Errorf("extra update actions: %#v", extra.GetObject())
		}
	}
}

func assertPatches(
	t *testing.T,
	expected Patches,
	patches []clientgotesting.PatchAction,
	expectedNamespace string,
) {

	for i, want := range expected {
		if i >= len(patches) {
			t.Errorf("missing patch: %v", prettyPatch(want))
			continue
		}

		got := patches[i]

		if got.GetName() != want.GetName() {
			t.Errorf("unexpected patch action[%d]: %s", i, prettyPatch(got))
		}

		if got.GetNamespace() != want.GetNamespace() {
			t.Errorf("unexpected patch action[%d]: %s", i, prettyPatch(got))
		}

		if diff := cmp.Diff(want.GetPatch(), got.GetPatch()); diff != "" {
			t.Errorf("unexpected patch diff (-want +got): %s", diff)
		}
	}

	if got, want := len(patches), len(expected); got > want {
		for _, extra := range patches[want:] {
			t.Errorf("extra patch actions: %s", prettyPatch(extra))
		}
	}
}

func setupClientValidations(clients []FakeClient, failures Failures) ActionRecorderList {
	var recorders ActionRecorderList

	for _, client := range clients {
		recorders = append(recorders, client)

		client.PrependReactor("create", "*", ValidateCreates)
		client.PrependReactor("update", "*", ValidateUpdates)

		for _, failure := range failures {
			client.PrependReactor("*", "*", failure)
		}
	}

	return recorders
}

func prettyPatch(patch clientgotesting.PatchAction) string {
	return fmt.Sprintf("resource: %q - name: %q - namespace: %q - patch: %s",
		patch.GetResource().Resource,
		patch.GetName(),
		patch.GetNamespace(),
		string(patch.GetPatch()),
	)
}

var cmpOpts = cmp.Options{
	cmpopts.EquateEmpty(),
	safeDeployDiff,
	ignoreLastTransitionTime,
}
