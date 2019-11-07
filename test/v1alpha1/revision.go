/*
Copyright 2019 The Knative Authors

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
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/pkg/test/logging"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/test"
)

// WaitForRevisionState polls the status of the Revision called name
// from client every `PollInterval` until `inState` returns `true` indicating it
// is done, returns an error or PollTimeout. desc will be used to name the metric
// that is emitted to track how long it took for name to get into the state checked by inState.
func WaitForRevisionState(client *test.ServingAlphaClients, name string, inState func(r *v1alpha1.Revision) (bool, error), desc string) error {
	span := logging.GetEmitableSpan(context.Background(), fmt.Sprintf("WaitForRevision/%s/%s", name, desc))
	defer span.End()

	var lastState *v1alpha1.Revision
	waitErr := wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		var err error
		lastState, err = client.Revisions.Get(name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		return inState(lastState)
	})

	if waitErr != nil {
		return fmt.Errorf("revision %q is not in desired state, got: %+v: %w", name, lastState, waitErr)
	}
	return nil
}

// CheckRevisionState verifies the status of the Revision called name from client
// is in a particular state by calling `inState` and expecting `true`.
// This is the non-polling variety of WaitForRevisionState
func CheckRevisionState(client *test.ServingAlphaClients, name string, inState func(r *v1alpha1.Revision) (bool, error)) error {
	r, err := client.Revisions.Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if done, err := inState(r); err != nil {
		return err
	} else if !done {
		return fmt.Errorf("revision %q is not in desired state, got: %+v", name, r)
	}
	return nil
}

// IsRevisionReady will check the status conditions of the revision and return true if the revision is
// ready to serve traffic. It will return false if the status indicates a state other than deploying
// or being ready. It will also return false if the type of the condition is unexpected.
func IsRevisionReady(r *v1alpha1.Revision) (bool, error) {
	return r.Generation == r.Status.ObservedGeneration && r.Status.IsReady(), nil
}

// IsRevisionPinned will check if the revision is pinned to a route.
func IsRevisionPinned(r *v1alpha1.Revision) (bool, error) {
	_, pinned := r.Annotations[serving.RevisionLastPinnedAnnotationKey]
	return pinned, nil
}

// IsRevisionAtExpectedGeneration returns a function that will check if the annotations
// on the revision include an annotation for the generation and that the annotation is
// set to the expected value.
func IsRevisionAtExpectedGeneration(expectedGeneration string) func(r *v1alpha1.Revision) (bool, error) {
	return func(r *v1alpha1.Revision) (bool, error) {
		if a, ok := r.Labels[serving.ConfigurationGenerationLabelKey]; ok {
			if a != expectedGeneration {
				return true, fmt.Errorf("expected Revision %s to be labeled with generation %s but was %s instead", r.Name, expectedGeneration, a)
			}
			return true, nil
		}
		return true, fmt.Errorf("expected Revision %s to be labeled with generation %s but there was no label", r.Name, expectedGeneration)
	}
}
