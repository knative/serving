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
	"time"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RevisionOption enables further configuration of a Revision.
type RevisionOption func(*v1alpha1.Revision)

// WithRevisionDeletionTimestamp will set the DeletionTimestamp on the Revision.
func WithRevisionDeletionTimestamp(r *v1alpha1.Revision) {
	t := metav1.NewTime(time.Unix(1e9, 0))
	r.ObjectMeta.SetDeletionTimestamp(&t)
}

// WithInitRevConditions calls .Status.InitializeConditions() on a Revision.
func WithInitRevConditions(r *v1alpha1.Revision) {
	r.Status.InitializeConditions()
}

// WithRevName sets the name of the revision
func WithRevName(name string) RevisionOption {
	return func(rev *v1alpha1.Revision) {
		rev.Name = name
	}
}

// WithServiceName propagates the given service name to the revision status.
func WithServiceName(sn string) RevisionOption {
	return func(rev *v1alpha1.Revision) {
		rev.Status.ServiceName = sn
	}
}

// MarkResourceNotOwned calls the function of the same name on the Revision's status.
func MarkResourceNotOwned(kind, name string) RevisionOption {
	return func(rev *v1alpha1.Revision) {
		rev.Status.MarkResourceNotOwned(kind, name)
	}
}

// WithRevContainerConcurrency sets the given Revision's concurrency.
func WithRevContainerConcurrency(cc v1beta1.RevisionContainerConcurrencyType) RevisionOption {
	return func(rev *v1alpha1.Revision) {
		rev.Spec.ContainerConcurrency = cc
	}
}

// WithLogURL sets the .Status.LogURL to the expected value.
func WithLogURL(r *v1alpha1.Revision) {
	r.Status.LogURL = "http://logger.io/test-uid"
}

// WithCreationTimestamp sets the Revision's timestamp to the provided time.
// TODO(mattmoor): Ideally this could be a more generic Option and use meta.Accessor,
// but unfortunately Go's type system cannot support that.
func WithCreationTimestamp(t time.Time) RevisionOption {
	return func(rev *v1alpha1.Revision) {
		rev.ObjectMeta.CreationTimestamp = metav1.Time{Time: t}
	}
}

// WithLastPinned updates the "last pinned" annotation to the provided timestamp.
func WithLastPinned(t time.Time) RevisionOption {
	return func(rev *v1alpha1.Revision) {
		rev.SetLastPinned(t)
	}
}

// WithRevStatus is a generic escape hatch for creating hard-to-craft
// status orientations.
func WithRevStatus(st v1alpha1.RevisionStatus) RevisionOption {
	return func(rev *v1alpha1.Revision) {
		rev.Status = st
	}
}

// MarkActive calls .Status.MarkActive on the Revision.
func MarkActive(r *v1alpha1.Revision) {
	r.Status.MarkActive()
}

// MarkInactive calls .Status.MarkInactive on the Revision.
func MarkInactive(reason, message string) RevisionOption {
	return func(r *v1alpha1.Revision) {
		r.Status.MarkInactive(reason, message)
	}
}

// MarkActivating calls .Status.MarkActivating on the Revision.
func MarkActivating(reason, message string) RevisionOption {
	return func(r *v1alpha1.Revision) {
		r.Status.MarkActivating(reason, message)
	}
}

// MarkDeploying calls .Status.MarkDeploying on the Revision.
func MarkDeploying(reason string) RevisionOption {
	return func(r *v1alpha1.Revision) {
		r.Status.MarkDeploying(reason)
	}
}

// MarkProgressDeadlineExceeded calls the method of the same name on the Revision
// with the message we expect the Revision Reconciler to pass.
func MarkProgressDeadlineExceeded(r *v1alpha1.Revision) {
	r.Status.MarkProgressDeadlineExceeded("Unable to create pods for more than 120 seconds.")
}

// MarkServiceTimeout calls .Status.MarkServiceTimeout on the Revision.
func MarkServiceTimeout(r *v1alpha1.Revision) {
	r.Status.MarkServiceTimeout()
}

// MarkContainerMissing calls .Status.MarkContainerMissing on the Revision.
func MarkContainerMissing(rev *v1alpha1.Revision) {
	rev.Status.MarkContainerMissing("It's the end of the world as we know it")
}

// MarkContainerExiting calls .Status.MarkContainerExiting on the Revision.
func MarkContainerExiting(exitCode int32, message string) RevisionOption {
	return func(r *v1alpha1.Revision) {
		r.Status.MarkContainerExiting(exitCode, message)
	}
}

// MarkResourcesUnavailable calls .Status.MarkResourcesUnavailable on the Revision.
func MarkResourcesUnavailable(reason, message string) RevisionOption {
	return func(r *v1alpha1.Revision) {
		r.Status.MarkResourcesUnavailable(reason, message)
	}
}

// MarkRevisionReady calls the necessary helpers to make the Revision Ready=True.
func MarkRevisionReady(r *v1alpha1.Revision) {
	WithInitRevConditions(r)
	MarkActive(r)
	r.Status.MarkResourcesAvailable()
	r.Status.MarkContainerHealthy()
}
