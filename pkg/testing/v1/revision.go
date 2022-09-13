/*
Copyright 2020 The Knative Authors

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

package v1

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/clock"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

// RevisionOption enables further configuration of a Revision.
type RevisionOption func(*v1.Revision)

// WithRevisionDeletionTimestamp will set the DeletionTimestamp on the Revision.
func WithRevisionDeletionTimestamp(r *v1.Revision) {
	t := metav1.NewTime(time.Unix(1e9, 0))
	r.ObjectMeta.SetDeletionTimestamp(&t)
}

// WithInitRevConditions calls .Status.InitializeConditions() on a Revision.
func WithInitRevConditions(r *v1.Revision) {
	r.Status.InitializeConditions()
}

// WithRevName sets the name of the revision
func WithRevName(name string) RevisionOption {
	return func(rev *v1.Revision) {
		rev.Name = name
	}
}

// MarkResourceNotOwned calls the function of the same name on the Revision's status.
func MarkResourceNotOwned(kind, name string) RevisionOption {
	return func(rev *v1.Revision) {
		rev.Status.MarkResourcesAvailableFalse(
			v1.ReasonNotOwned,
			v1.ResourceNotOwnedMessage(kind, name),
		)
	}
}

// WithRevContainerConcurrency sets the given Revision's concurrency.
func WithRevContainerConcurrency(cc int64) RevisionOption {
	return func(rev *v1.Revision) {
		rev.Spec.ContainerConcurrency = &cc
	}
}

// WithLogURL sets the .Status.LogURL to the expected value.
func WithLogURL(r *v1.Revision) {
	r.Status.LogURL = "http://logger.io/test-uid"
}

// WithCreationTimestamp sets the Revision's timestamp to the provided time.
// TODO(mattmoor): Ideally this could be a more generic Option and use meta.Accessor,
// but unfortunately Go's type system cannot support that.
func WithCreationTimestamp(t time.Time) RevisionOption {
	return func(rev *v1.Revision) {
		rev.ObjectMeta.CreationTimestamp = metav1.Time{Time: t}
	}
}

// WithRevisionPreserveAnnotation updates the annotation with preserve key.
func WithRevisionPreserveAnnotation() RevisionOption {
	return func(rev *v1.Revision) {
		rev.Annotations = kmeta.UnionMaps(rev.Annotations,
			map[string]string{
				serving.RevisionPreservedAnnotationKey: "true",
			})
	}
}

// WithRoutingStateModified updates the annotation to the provided timestamp.
func WithRoutingStateModified(t time.Time) RevisionOption {
	return func(rev *v1.Revision) {
		rev.Annotations = kmeta.UnionMaps(rev.Annotations,
			map[string]string{
				serving.RoutingStateModifiedAnnotationKey: t.UTC().Format(time.RFC3339),
			})
	}
}

// WithRoutingState updates the annotation to the provided timestamp.
func WithRoutingState(s v1.RoutingState, c clock.PassiveClock) RevisionOption {
	return func(rev *v1.Revision) {
		rev.SetRoutingState(s, c.Now())
	}
}

// WithRevStatus is a generic escape hatch for creating hard-to-craft
// status orientations.
func WithRevStatus(st v1.RevisionStatus) RevisionOption {
	return func(rev *v1.Revision) {
		rev.Status = st
	}
}

// WithImagePullSecrets updates the revision spec ImagePullSecrets to
// the provided secrets
func WithImagePullSecrets(secretName string) RevisionOption {
	return func(rev *v1.Revision) {
		rev.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{
			Name: secretName,
		}}
	}
}

// MarkActive calls .Status.MarkActive on the Revision.
func MarkActive(r *v1.Revision) {
	r.Status.MarkActiveTrue()
}

// MarkInactive calls .Status.MarkInactive on the Revision.
func MarkInactive(reason, message string) RevisionOption {
	return func(r *v1.Revision) {
		r.Status.MarkActiveFalse(reason, message)
	}
}

// MarkActivating calls .Status.MarkActivating on the Revision.
func MarkActivating(reason, message string) RevisionOption {
	return func(r *v1.Revision) {
		r.Status.MarkActiveUnknown(reason, message)
	}
}

// MarkDeploying calls .Status.MarkDeploying on the Revision.
func MarkDeploying(reason string) RevisionOption {
	return func(r *v1.Revision) {
		r.Status.MarkResourcesAvailableUnknown(reason, "")
		r.Status.MarkContainerHealthyUnknown(reason, "")
	}
}

// MarkProgressDeadlineExceeded calls the method of the same name on the Revision
// with the message we expect the Revision Reconciler to pass.
func MarkProgressDeadlineExceeded(message string) RevisionOption {
	return func(r *v1.Revision) {
		r.Status.MarkResourcesAvailableFalse(
			v1.ReasonProgressDeadlineExceeded,
			message,
		)
	}
}

// MarkContainerMissing calls .Status.MarkContainerMissing on the Revision.
func MarkContainerMissing(rev *v1.Revision) {
	rev.Status.MarkContainerHealthyFalse(v1.ReasonContainerMissing, "It's the end of the world as we know it")
}

// MarkContainerExiting calls .Status.MarkContainerExiting on the Revision.
func MarkContainerExiting(exitCode int32, message string) RevisionOption {
	return func(r *v1.Revision) {
		r.Status.MarkContainerHealthyFalse(v1.ExitCodeReason(exitCode), message)
	}
}

// MarkResourcesUnavailable calls .Status.MarkResourcesUnavailable on the Revision.
func MarkResourcesUnavailable(reason, message string) RevisionOption {
	return func(r *v1.Revision) {
		r.Status.MarkResourcesAvailableFalse(reason, message)
	}
}

// MarkRevisionReady calls the necessary helpers to make the Revision Ready=True.
func MarkRevisionReady(r *v1.Revision) {
	WithInitRevConditions(r)
	MarkActive(r)
	r.Status.MarkResourcesAvailableTrue()
	r.Status.MarkContainerHealthyTrue()
	r.Status.ObservedGeneration = r.Generation
}

// WithRevisionLabel attaches a particular label to the revision.
func WithRevisionLabel(key, value string) RevisionOption {
	return func(rev *v1.Revision) {
		rev.Labels = kmeta.UnionMaps(rev.Labels, map[string]string{key: value})
	}
}

// WithRevisionAnn attaches a particular label to the revision.
func WithRevisionAnn(key, value string) RevisionOption {
	return func(rev *v1.Revision) {
		rev.Annotations = kmeta.UnionMaps(rev.Annotations, map[string]string{key: value})
	}
}

// WithContainerStatuses sets the .Status.ContainerStatuses to the Revision.
func WithContainerStatuses(containerStatus []v1.ContainerStatus) RevisionOption {
	return func(r *v1.Revision) {
		r.Status.ContainerStatuses = containerStatus
	}
}

// WithRevisionObservedGeneration sets the observed generation on the
// revision status.
func WithRevisionObservedGeneration(gen int64) RevisionOption {
	return func(r *v1.Revision) {
		r.Status.ObservedGeneration = gen
	}
}

func WithRevisionInitContainers() RevisionOption {
	return func(r *v1.Revision) {
		r.Spec.InitContainers = []corev1.Container{{
			Name:  "init1",
			Image: "initimage",
		}, {
			Name:  "init2",
			Image: "initimage",
		}}
	}
}

func WithRevisionPVC() RevisionOption {
	return func(r *v1.Revision) {
		r.Spec.Volumes = []corev1.Volume{{
			Name: "claimvolume",
			VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: "myclaim",
				ReadOnly:  false,
			}}},
		}
		r.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{
			Name:      "claimvolume",
			MountPath: "/data",
		}}
	}
}

// Revision creates a revision object with given ns/name and options.
func Revision(namespace, name string, ro ...RevisionOption) *v1.Revision {
	r := &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:   "/apis/serving/v1/namespaces/test/revisions/" + name,
			Name:       name,
			Namespace:  namespace,
			UID:        "test-uid",
			Generation: 1,
		},
		Spec: v1.RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  name,
					Image: "busybox",
				}},
			},
		},
	}
	r.SetDefaults(context.Background())

	for _, opt := range ro {
		opt(r)
	}
	return r
}
