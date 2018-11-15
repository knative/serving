/*
Copyright 2018 The Knative Authors

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
	"time"

	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	kpav1alpha1 "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	confignames "github.com/knative/serving/pkg/reconciler/v1alpha1/configuration/resources/names"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// BuildOption enables further configuration of a Build.
type BuildOption func(*unstructured.Unstructured)

// ServiceOption enables further configuration of a Service.
type ServiceOption func(*v1alpha1.Service)

var (
	// configSpec is the spec used for the different styles of Service rollout.
	configSpec = v1alpha1.ConfigurationSpec{
		RevisionTemplate: v1alpha1.RevisionTemplateSpec{
			Spec: v1alpha1.RevisionSpec{
				Container: corev1.Container{
					Image: "busybox",
				},
			},
		},
	}
)

// RouteOption enables further configuration of a Route.
type RouteOption func(*v1alpha1.Route)

// ConfigOption enables further configuration of a Configuration.
type ConfigOption func(*v1alpha1.Configuration)

// WithBuild adds a Build to the provided Configuration.
func WithBuild(cfg *v1alpha1.Configuration) {
	cfg.Spec.Build = &v1alpha1.RawExtension{
		Object: &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "testing.build.knative.dev/v1alpha1",
				"kind":       "Build",
				"spec": map[string]interface{}{
					"steps": []interface{}{
						map[string]interface{}{
							"image": "foo",
						},
						map[string]interface{}{
							"image": "bar",
						},
					},
				},
			},
		},
	}
}

// WithConfigConcurrencyModel sets the given Configuration's concurrency model.
func WithConfigConcurrencyModel(ss v1alpha1.RevisionRequestConcurrencyModelType) ConfigOption {
	return func(cfg *v1alpha1.Configuration) {
		cfg.Spec.RevisionTemplate.Spec.ConcurrencyModel = ss
	}
}

// WithGeneration sets the generation of the Configuration.
func WithGeneration(gen int64) ConfigOption {
	return func(cfg *v1alpha1.Configuration) {
		cfg.Spec.Generation = gen
	}
}

// WithObservedGen sets the observed generation of the Configuration.
func WithObservedGen(cfg *v1alpha1.Configuration) {
	cfg.Status.ObservedGeneration = cfg.Spec.Generation
}

// WithLatestCreated initializes the .status.latestCreatedRevisionName to be the name
// of the latest revision that the Configuration would have created.
func WithLatestCreated(cfg *v1alpha1.Configuration) {
	cfg.Status.SetLatestCreatedRevisionName(confignames.Revision(cfg))
}

// WithLatestReady initializes the .status.latestReadyRevisionName to be the name
// of the latest revision that the Configuration would have created.
func WithLatestReady(cfg *v1alpha1.Configuration) {
	cfg.Status.SetLatestReadyRevisionName(confignames.Revision(cfg))
}

// MarkRevisionCreationFailed calls .Status.MarkRevisionCreationFailed.
func MarkRevisionCreationFailed(msg string) ConfigOption {
	return func(cfg *v1alpha1.Configuration) {
		cfg.Status.MarkRevisionCreationFailed(msg)
	}
}

// MarkLatestCreatedFailed calls .Status.MarkLatestCreatedFailed.
func MarkLatestCreatedFailed(msg string) ConfigOption {
	return func(cfg *v1alpha1.Configuration) {
		cfg.Status.MarkLatestCreatedFailed(cfg.Status.LatestCreatedRevisionName, msg)
	}
}

// RevisionOption enables further configuration of a Revision.
type RevisionOption func(*v1alpha1.Revision)

// WithInitRevConditions calls .Status.InitializeConditions() on a Revision.
func WithInitRevConditions(r *v1alpha1.Revision) {
	r.Status.InitializeConditions()
}

// WithBuildRef sets the .Spec.BuildRef on the Revision to match what we'd get
// using WithBuild(name).
func WithBuildRef(name string) RevisionOption {
	return func(rev *v1alpha1.Revision) {
		rev.Spec.BuildRef = &corev1.ObjectReference{
			APIVersion: "testing.build.knative.dev/v1alpha1",
			Kind:       "Build",
			Name:       name,
		}
	}
}

// WithRevConcurrencyModel sets the concurrency model on the Revision.
func WithRevConcurrencyModel(ss v1alpha1.RevisionRequestConcurrencyModelType) RevisionOption {
	return func(rev *v1alpha1.Revision) {
		rev.Spec.ConcurrencyModel = ss
	}
}

// WithCreationTimestamp sets the Revision's timestamp to the provided time.
// TODO(mattmoor): Ideally this could be a more generic Option and use meta.Accessor,
// but unfortunately Go's type system cannot support that.
func WithCreationTimestamp(t time.Time) RevisionOption {
	return func(rev *v1alpha1.Revision) {
		rev.ObjectMeta.CreationTimestamp = metav1.Time{t}
	}
}

// WithNoBuild updates the status conditions to propagate a Build status as-if
// no BuildRef was specified.
func WithNoBuild(r *v1alpha1.Revision) {
	r.Status.PropagateBuildStatus(duckv1alpha1.KResourceStatus{
		Conditions: []duckv1alpha1.Condition{{
			Type:   duckv1alpha1.ConditionSucceeded,
			Status: corev1.ConditionTrue,
			Reason: "NoBuild",
		}},
	})
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

// MarkContainerMissing calls .Status.MarkContainerMissing on the Revision.
func MarkContainerMissing(rev *v1alpha1.Revision) {
	rev.Status.MarkContainerMissing("It's the end of the world as we know it")
}

// MarkRevisionReady calls the necessary helpers to make the Revision Ready=True.
func MarkRevisionReady(r *v1alpha1.Revision) {
	WithInitRevConditions(r)
	WithNoBuild(r)
	MarkActive(r)
	r.Status.MarkResourcesAvailable()
	r.Status.MarkContainerHealthy()
}

// KPAOption enables further configuration of the PodAutoscaler.
type KPAOption func(*kpav1alpha1.PodAutoscaler)

// K8sServiceOption enables further configuration of the Kubernetes Service.
type K8sServiceOption func(*corev1.Service)

// EndpointsOption enables further configuration of the Kubernetes Endpoints.
type EndpointsOption func(*corev1.Endpoints)
