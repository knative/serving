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
	"testing"

	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/reconciler"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	. "github.com/knative/pkg/logging/testing"
)

type (
	// ReconcilerSetupFunc is responsible for creating a reconciler and setting up
	// any various clients and informers with the given runtime objects.
	//
	// The function should return the list of fake clientsets
	// so the test can assert on create, update and patch actions.
	//
	// ReconcilerTest will also prepend validation and failure reactors.
	// These failure reactors can be set on the ReconcilerTest's Failures
	// property
	ReconcilerSetupFunc func(reconciler.CommonOptions, []runtime.Object) (reconciler.Reconciler, []FakeClient)

	ReconcilerTests []ReconcilerTest

	// ReconcilerTest is used to test a single reconciler reconciliation.
	ReconcilerTest struct {
		Name    string
		Key     string
		Context context.Context

		// World State
		Failures Failures
		Objects  Objects
		//
		// Expectations
		ExpectedCreates Creates
		ExpectedPatches Patches
		ExpectedUpdates Updates
		ExpectError     bool
	}
)

// Run will iterate over each ReconcilerTest and invoke them as a subtest.
func (tests ReconcilerTests) Run(t *testing.T, setup ReconcilerSetupFunc) {
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Run(t, setup)
		})
	}
}

// Run will setup the ReconcilerTest, trigger a reconcile and perform
// the necessary test assertions.
func (s *ReconcilerTest) Run(t *testing.T, setup ReconcilerSetupFunc) {
	logger := TestLogger(t)

	opts := reconciler.CommonOptions{
		Logger:           TestLogger(t),
		Recorder:         &record.FakeRecorder{},
		ObjectTracker:    &NullTracker{},
		ConfigMapWatcher: &FakeConfigMapWatcher{},
		WorkQueue:        &FakeWorkQueue{},
	}

	reconciler, fakeClients := setup(opts, s.Objects)

	clients := setupClientValidations(fakeClients, s.Failures)

	ctx := context.TODO()

	if s.Context != nil {
		ctx = s.Context
	}

	ctx = logging.WithLogger(ctx, logger)

	err := reconciler.Reconcile(ctx, s.Key)

	if (err != nil) != s.ExpectError {
		t.Errorf("Reconcile() error = %v, expected error %v", err, s.ExpectError)
	}

	actions, err := clients.ActionsByVerb()

	if err != nil {
		t.Errorf("error capturing actions by verb: %q", err)
	}

	expectedNamespace, _, _ := cache.SplitMetaNamespaceKey(s.Key)

	assertCreates(t, s.ExpectedCreates, actions.Creates, expectedNamespace)
	assertUpdates(t, s.ExpectedUpdates, actions.Updates, expectedNamespace)
	assertPatches(t, s.ExpectedPatches, actions.Patches, expectedNamespace)
}
