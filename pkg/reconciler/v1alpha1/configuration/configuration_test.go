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

package configuration

import (
	"context"
	"fmt"
	"testing"
	"time"

	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/gc"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/configuration/config"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/configuration/resources"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgotesting "k8s.io/client-go/testing"

	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
)

var (
	boolTrue     = true
	revisionSpec = v1alpha1.RevisionSpec{
		Container: corev1.Container{
			Image: "busybox",
		},
		TimeoutSeconds: 60,
	}
)

// This is heavily based on the way the OpenShift Ingress controller tests its reconciliation method.
func TestReconcile(t *testing.T) {
	now := time.Now()

	table := TableTest{{
		Name: "bad workqueue key",
		Key:  "too/many/parts",
	}, {
		Name: "key not found",
		Key:  "foo/not-found",
	}, {
		Name: "create revision matching generation",
		Objects: []runtime.Object{
			cfg("no-revisions-yet", "foo", 1234),
		},
		WantCreates: []metav1.Object{
			rev("no-revisions-yet", "foo", 1234),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("no-revisions-yet", "foo", 1234,
				// The following properties are set when we first reconcile a
				// Configuration and a Revision is created.
				WithLatestCreated, WithObservedGen),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Revision %q", "no-revisions-yet-01234"),
		},
		Key: "foo/no-revisions-yet",
	}, {
		Name: "webhook validation failure",
		// If we attempt to create a Revision with a bad ConcurrencyModel set, we fail.
		WantErr: true,
		Objects: []runtime.Object{
			cfg("validation-failure", "foo", 1234, WithConfigConcurrencyModel("Bogus")),
		},
		WantCreates: []metav1.Object{
			rev("validation-failure", "foo", 1234, WithRevConcurrencyModel("Bogus")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("validation-failure", "foo", 1234, WithConfigConcurrencyModel("Bogus"),
				// Expect Revision creation to fail with the following error.
				MarkRevisionCreationFailed(`invalid value "Bogus": spec.concurrencyModel`)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "CreationFailed", "Failed to create Revision %q: %v",
				"validation-failure-01234", `invalid value "Bogus": spec.concurrencyModel`),
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "Failed to update status for Configuration %q: %v",
				"validation-failure", `invalid value "Bogus": spec.revisionTemplate.spec.concurrencyModel`),
		},
		Key: "foo/validation-failure",
	}, {
		Name: "elide build when a matching one already exists",
		Objects: []runtime.Object{
			cfg("need-rev-and-build", "foo", 99998, WithBuild),
			// An existing build is reused!
			resources.MakeBuild(cfg("something-else", "foo", 12345, WithBuild)),
		},
		WantCreates: []metav1.Object{
			rev("need-rev-and-build", "foo", 99998, WithBuildRef("something-else-12345")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("need-rev-and-build", "foo", 99998, WithBuild,
				// The following properties are set when we first reconcile a Configuration
				// that stamps out a Revision with an existing Build.
				WithLatestCreated, WithObservedGen),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Revision %q", "need-rev-and-build-99998"),
		},
		Key: "foo/need-rev-and-build",
	}, {
		Name: "create revision matching generation with build",
		Objects: []runtime.Object{
			cfg("need-rev-and-build", "foo", 99998, WithBuild),
		},
		WantCreates: []metav1.Object{
			resources.MakeBuild(cfg("need-rev-and-build", "foo", 99998, WithBuild)),
			rev("need-rev-and-build", "foo", 99998, WithBuildRef("need-rev-and-build-99998")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("need-rev-and-build", "foo", 99998, WithBuild,
				// The following properties are set when we first reconcile a Configuration
				// that stamps our a Revision and a Build.
				WithLatestCreated, WithObservedGen),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Build %q", "need-rev-and-build-99998"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Revision %q", "need-rev-and-build-99998"),
		},
		Key: "foo/need-rev-and-build",
	}, {
		Name: "reconcile revision matching generation (ready: unknown)",
		Objects: []runtime.Object{
			cfg("matching-revision-not-done", "foo", 5432),
			rev("matching-revision-not-done", "foo", 5432, WithCreationTimestamp(now)),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("matching-revision-not-done", "foo", 5432,
				// If the Revision already exists, we still update these fields.
				// This could happen if the prior status update failed for some reason.
				WithLatestCreated, WithObservedGen),
		}},
		Key: "foo/matching-revision-not-done",
	}, {
		Name: "reconcile revision matching generation (ready: true)",
		Objects: []runtime.Object{
			cfg("matching-revision-done", "foo", 5555, WithLatestCreated, WithObservedGen),
			rev("matching-revision-done", "foo", 5555,
				WithCreationTimestamp(now), MarkRevisionReady),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("matching-revision-done", "foo", 5555, WithLatestCreated, WithObservedGen,
				// When we see the LatestCreatedRevision become Ready, then we
				// update the latest ready revision.
				WithLatestReady),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "ConfigurationReady", "Configuration becomes ready"),
			Eventf(corev1.EventTypeNormal, "LatestReadyUpdate", "LatestReadyRevisionName updated to %q",
				"matching-revision-done-05555"),
		},
		Key: "foo/matching-revision-done",
	}, {
		Name: "reconcile a ready revision without the config metadata generation label",
		Objects: []runtime.Object{
			cfg("legacy-revision-ready", "foo", 5555,
				WithLatestCreated,
				WithObservedGen,
				WithLatestReady),
			rev("legacy-revision-ready", "foo", 5555,
				WithCreationTimestamp(now),
				MarkRevisionReady,
				WithoutConfigurationMetadataGenerationLabel,
			),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("legacy-revision-ready", "foo", 5555,
				WithCreationTimestamp(now),
				MarkRevisionReady,
			),
		}},
		Key: "foo/legacy-revision-ready",
	}, {
		Name: "reconcile revision matching generation (ready: true, idempotent)",
		Objects: []runtime.Object{
			cfg("matching-revision-done-idempotent", "foo", 5566,
				WithLatestCreated, WithObservedGen, WithLatestReady),
			rev("matching-revision-done-idempotent", "foo", 5566,
				WithCreationTimestamp(now), MarkRevisionReady),
		},
		Key: "foo/matching-revision-done-idempotent",
	}, {
		Name: "reconcile revision matching generation (ready: false)",
		Objects: []runtime.Object{
			cfg("matching-revision-failed", "foo", 5555, WithLatestCreated, WithObservedGen),
			rev("matching-revision-failed", "foo", 5555,
				WithCreationTimestamp(now), MarkContainerMissing),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("matching-revision-failed", "foo", 5555,
				WithLatestCreated, WithObservedGen,
				// When the LatestCreatedRevision reports back a failure,
				// then we surface that failure.
				MarkLatestCreatedFailed("It's the end of the world as we know it")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "LatestCreatedFailed", "Latest created revision %q has failed",
				"matching-revision-failed-05555"),
		},
		Key: "foo/matching-revision-failed",
	}, {
		Name: "reconcile revision matching generation (ready: bad)",
		Objects: []runtime.Object{
			cfg("bad-condition", "foo", 5555, WithLatestCreated, WithObservedGen),
			rev("bad-condition", "foo", 5555, WithRevStatus(v1alpha1.RevisionStatus{
				Conditions: duckv1alpha1.Conditions{{
					Type:     v1alpha1.RevisionConditionReady,
					Status:   "Bad",
					Severity: "Error",
				}},
			})),
		},
		WantErr: true,
		Key:     "foo/bad-condition",
	}, {
		Name: "failure creating build",
		// We induce a failure creating a build
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "builds"),
		},
		Objects: []runtime.Object{
			cfg("create-build-failure", "foo", 99998, WithBuild),
		},
		WantCreates: []metav1.Object{
			resources.MakeBuild(cfg("create-build-failure", "foo", 99998, WithBuild)),
			// No Revision gets created.
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("create-build-failure", "foo", 99998, WithBuild,
				// When we fail to create a Build it should be surfaced in
				// the Configuration status.
				MarkRevisionCreationFailed("inducing failure for create builds")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "CreationFailed", "Failed to create Revision %q: %v",
				"create-build-failure-99998", "inducing failure for create builds"),
		},
		Key: "foo/create-build-failure",
	}, {
		Name: "failure creating revision",
		// We induce a failure creating a revision
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "revisions"),
		},
		Objects: []runtime.Object{
			cfg("create-revision-failure", "foo", 99998),
		},
		WantCreates: []metav1.Object{
			rev("create-revision-failure", "foo", 99998),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("create-revision-failure", "foo", 99998,
				// When we fail to create a Revision is should be surfaced in
				// the Configuration status.
				MarkRevisionCreationFailed("inducing failure for create revisions")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "CreationFailed", "Failed to create Revision %q: %v",
				"create-revision-failure-99998", "inducing failure for create revisions"),
		},
		Key: "foo/create-revision-failure",
	}, {
		Name: "failure updating configuration status",
		// Induce a failure updating the status of the configuration.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "configurations"),
		},
		Objects: []runtime.Object{
			cfg("update-config-failure", "foo", 1234),
		},
		WantCreates: []metav1.Object{
			rev("update-config-failure", "foo", 1234),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("update-config-failure", "foo", 1234,
				// These would be the status updates after a first
				// reconcile, which we use to trigger the update
				// where we've induced a failure.
				WithLatestCreated, WithObservedGen),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Revision %q", "update-config-failure-01234"),
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "Failed to update status for Configuration %q: %v",
				"update-config-failure", "inducing failure for update configurations"),
		},
		Key: "foo/update-config-failure",
	}, {
		Name: "failed revision recovers",
		Objects: []runtime.Object{
			cfg("revision-recovers", "foo", 1337,
				WithLatestCreated, WithLatestReady, WithObservedGen,
				MarkLatestCreatedFailed("Weebles wobble, but they don't fall down")),
			rev("revision-recovers", "foo", 1337,
				WithCreationTimestamp(now), MarkRevisionReady),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("revision-recovers", "foo", 1337,
				WithLatestCreated, WithLatestReady, WithObservedGen,
				// When a LatestReadyRevision recovers from failure,
				// then we should go back to Ready.
			),
		}},
		Key: "foo/revision-recovers",
	}, {
		// The name is a bit misleading but essentially we are testing that
		// querying the latest created revision includes the configuration name
		// as part of the selector
		Name: "two steady state configs with same generation should be a noop",
		Objects: []runtime.Object{
			// double-trouble needs to be first for this test to fail
			// when no fix is present
			cfg("double-trouble", "foo", 1,
				WithLatestCreated, WithLatestReady, WithObservedGen),
			cfg("first-trouble", "foo", 1,
				WithLatestCreated, WithLatestReady, WithObservedGen),

			rev("first-trouble", "foo", 1,
				WithCreationTimestamp(now), MarkRevisionReady),
			rev("double-trouble", "foo", 1,
				WithCreationTimestamp(now), MarkRevisionReady),
		},
		Key: "foo/double-trouble",
	}}

	table.Test(t, MakeFactory(func(listers *Listers, opt reconciler.Options) controller.Reconciler {
		return &Reconciler{
			Base:                reconciler.NewBase(opt, controllerAgentName),
			configurationLister: listers.GetConfigurationLister(),
			revisionLister:      listers.GetRevisionLister(),
			configStore: &testConfigStore{
				config: ReconcilerTestConfig(),
			},
		}
	}))
}

func TestGCReconcile(t *testing.T) {
	now := time.Now()
	tenMinutesAgo := now.Add(-10 * time.Minute)

	old := now.Add(-11 * time.Minute)
	older := now.Add(-12 * time.Minute)
	oldest := now.Add(-13 * time.Minute)

	table := TableTest{{
		Name: "delete oldest, keep two",
		Objects: []runtime.Object{
			cfg("keep-two", "foo", 5556,
				WithLatestCreated, WithObservedGen, WithLatestReady),
			rev("keep-two", "foo", 5554, MarkRevisionReady,
				WithCreationTimestamp(oldest),
				WithLastPinned(tenMinutesAgo)),
			rev("keep-two", "foo", 5555, MarkRevisionReady,
				WithCreationTimestamp(older),
				WithLastPinned(tenMinutesAgo)),
			rev("keep-two", "foo", 5556, MarkRevisionReady,
				WithCreationTimestamp(old),
				WithLastPinned(tenMinutesAgo)),
		},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: "foo",
				Verb:      "delete",
				Resource: schema.GroupVersionResource{
					Group:    "serving.knative.dev",
					Version:  "v1alpha1",
					Resource: "revisions",
				},
			},
			Name: "keep-two-05554",
		}},
		Key: "foo/keep-two",
	}, {
		Name: "keep oldest when no lastPinned",
		Objects: []runtime.Object{
			cfg("keep-no-last-pinned", "foo", 5556,
				WithLatestCreated, WithObservedGen, WithLatestReady),
			// No lastPinned so we will keep this.
			rev("keep-no-last-pinned", "foo", 5554, MarkRevisionReady,
				WithCreationTimestamp(oldest)),
			rev("keep-no-last-pinned", "foo", 5555, MarkRevisionReady,
				WithCreationTimestamp(older),
				WithLastPinned(tenMinutesAgo)),
			rev("keep-no-last-pinned", "foo", 5556, MarkRevisionReady,
				WithCreationTimestamp(old),
				WithLastPinned(tenMinutesAgo)),
		},
		Key: "foo/keep-no-last-pinned",
	}, {
		Name: "keep recent lastPinned",
		Objects: []runtime.Object{
			cfg("keep-recent-last-pinned", "foo", 5556,
				WithLatestCreated, WithObservedGen, WithLatestReady),
			rev("keep-recent-last-pinned", "foo", 5554, MarkRevisionReady,
				WithCreationTimestamp(oldest),
				// This is an indication that things are still routing here.
				WithLastPinned(now)),
			rev("keep-recent-last-pinned", "foo", 5555, MarkRevisionReady,
				WithCreationTimestamp(older),
				WithLastPinned(tenMinutesAgo)),
			rev("keep-recent-last-pinned", "foo", 5556, MarkRevisionReady,
				WithCreationTimestamp(old),
				WithLastPinned(tenMinutesAgo)),
		},
		Key: "foo/keep-recent-last-pinned",
	}, {
		Name: "keep LatestReadyRevision",
		Objects: []runtime.Object{
			// Create a revision where the LatestReady is 5554, but LatestCreated is 5556.
			// We should keep LatestReady even if it is old.
			cfg("keep-two", "foo", 5554,
				WithLatestCreated,
				WithLatestReady,
				WithGeneration(5556),
				WithLatestCreated,
				WithObservedGen),
			rev("keep-two", "foo", 5554, MarkRevisionReady,
				WithCreationTimestamp(oldest),
				WithLastPinned(tenMinutesAgo)),
			rev("keep-two", "foo", 5555, // Not Ready
				WithCreationTimestamp(older),
				WithLastPinned(tenMinutesAgo)),
			rev("keep-two", "foo", 5556, // Not Ready
				WithCreationTimestamp(old),
				WithLastPinned(tenMinutesAgo)),
		},
		Key: "foo/keep-two",
	}, {
		Name: "keep stale revision because of minimum generations",
		Objects: []runtime.Object{
			cfg("keep-all", "foo", 5554,
				// Don't set the latest ready revision here
				// since those by default are always retained
				WithLatestCreated,
				WithObservedGen),
			rev("keep-all", "foo", 5554,
				WithCreationTimestamp(oldest),
				WithLastPinned(tenMinutesAgo)),
		},
		Key: "foo/keep-all",
	}}

	table.Test(t, MakeFactory(func(listers *Listers, opt reconciler.Options) controller.Reconciler {
		return &Reconciler{
			Base:                reconciler.NewBase(opt, controllerAgentName),
			configurationLister: listers.GetConfigurationLister(),
			revisionLister:      listers.GetRevisionLister(),
			configStore: &testConfigStore{
				config: &config.Config{
					RevisionGC: &gc.Config{
						StaleRevisionCreateDelay:        5 * time.Minute,
						StaleRevisionTimeout:            5 * time.Minute,
						StaleRevisionMinimumGenerations: 2,
					},
				},
			},
		}
	}))
}

func cfg(name, namespace string, generation int64, co ...ConfigOption) *v1alpha1.Configuration {
	c := &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: generation,
		},
		Spec: v1alpha1.ConfigurationSpec{
			Generation: generation,
			RevisionTemplate: v1alpha1.RevisionTemplateSpec{
				Spec: revisionSpec,
			},
		},
	}
	for _, opt := range co {
		opt(c)
	}
	return c
}

func rev(name, namespace string, generation int64, ro ...RevisionOption) *v1alpha1.Revision {
	r := resources.MakeRevision(cfg(name, namespace, generation), nil)
	for _, opt := range ro {
		opt(r)
	}
	return r
}

type testConfigStore struct {
	config *config.Config
}

func (t *testConfigStore) ToContext(ctx context.Context) context.Context {
	return config.ToContext(ctx, t.config)
}

func (t *testConfigStore) WatchConfigs(w configmap.Watcher) {}

var _ configStore = (*testConfigStore)(nil)

func ReconcilerTestConfig() *config.Config {
	return &config.Config{
		RevisionGC: &gc.Config{
			StaleRevisionCreateDelay: 5 * time.Minute,
			StaleRevisionTimeout:     5 * time.Minute,
		},
	}
}

func TestIsRevisionStale(t *testing.T) {
	curTime := time.Now()
	staleTime := curTime.Add(-10 * time.Minute)

	tests := []struct {
		name      string
		rev       *v1alpha1.Revision
		latestRev string
		want      bool
	}{{
		name: "fresh revision that was never pinned",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(curTime),
			},
		},
		want: false,
	}, {
		name: "stale revision that was never pinned",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(staleTime),
			},
		},
		want: false,
	}, {
		name: "stale revision that was previously pinned",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(staleTime),
				Annotations: map[string]string{
					"serving.knative.dev/lastPinned": fmt.Sprintf("%d", staleTime.Unix()),
				},
			},
		},
		want: true,
	}, {
		name: "fresh revision that was previously pinned",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(staleTime),
				Annotations: map[string]string{
					"serving.knative.dev/lastPinned": fmt.Sprintf("%d", curTime.Unix()),
				},
			},
		},
		want: false,
	}, {
		name: "stale latest ready revision",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(staleTime),
				Annotations: map[string]string{
					"serving.knative.dev/lastPinned": fmt.Sprintf("%d", staleTime.Unix()),
				},
			},
		},
		latestRev: "myrev",
		want:      false,
	}}

	cfgStore := testConfigStore{
		config: &config.Config{
			RevisionGC: &gc.Config{
				StaleRevisionCreateDelay:        5 * time.Minute,
				StaleRevisionTimeout:            5 * time.Minute,
				StaleRevisionMinimumGenerations: 2,
			},
		},
	}
	ctx := cfgStore.ToContext(context.TODO())

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := &v1alpha1.Configuration{
				Status: v1alpha1.ConfigurationStatus{
					LatestReadyRevisionName: test.latestRev,
				},
			}

			got := isRevisionStale(ctx, test.rev, cfg)

			if got != test.want {
				t.Errorf("IsRevisionStale want %v got %v", test.want, got)
			}
		})
	}
}

// WithoutConfigurationMetadataGenerationLabel clears the label from the revision
func WithoutConfigurationMetadataGenerationLabel(rev *v1alpha1.Revision) {
	if rev.Labels == nil {
		return
	}

	delete(rev.Labels, serving.ConfigurationMetadataGenerationLabelKey)
}
