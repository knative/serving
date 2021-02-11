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

package configuration

import (
	"context"
	"errors"
	"testing"
	"time"

	// Inject the fake informers we need.
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/clock"
	clientgotesting "k8s.io/client-go/testing"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/ptr"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	servingclient "knative.dev/serving/pkg/client/injection/client/fake"
	configreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/configuration"
	"knative.dev/serving/pkg/reconciler/configuration/config"

	. "knative.dev/pkg/reconciler/testing"
	"knative.dev/serving/pkg/reconciler/configuration/resources"
	. "knative.dev/serving/pkg/reconciler/testing/v1"
	. "knative.dev/serving/pkg/testing/v1"
)

var revisionSpec = v1.RevisionSpec{
	PodSpec: corev1.PodSpec{
		Containers: []corev1.Container{{
			Image: "busybox",
		}},
	},
	TimeoutSeconds: ptr.Int64(60),
}

var testCtx context.Context
var testClock clock.PassiveClock

// This is heavily based on the way the OpenShift Ingress controller tests its reconciliation method.
func TestReconcile(t *testing.T) {
	testClock = clock.NewFakePassiveClock(time.Now())
	testCtx = context.Background()
	retryAttempted := false

	now := testClock.Now()

	table := TableTest{{
		Name: "bad workqueue key",
		Key:  "too/many/parts",
	}, {
		Name: "key not found",
		Key:  "foo/not-found",
	}, {
		Name: "nop deletion reconcile",
		// Test that with a DeletionTimestamp we do nothing.
		Objects: []runtime.Object{
			cfg("foo", "delete-pending", 1234, WithConfigDeletionTimestamp),
		},
		Key: "foo/delete-pending",
	}, {
		Name: "create revision matching generation, with retry",
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		Objects: []runtime.Object{
			cfg("no-revisions-yet", "foo", 1234),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
				if retryAttempted || !action.Matches("update", "configurations") || action.GetSubresource() != "status" {
					return false, nil, nil
				}
				retryAttempted = true
				return true, nil, apierrs.NewConflict(v1.Resource("foo"), "bar", errors.New("foo"))
			},
		},
		WantCreates: []runtime.Object{
			rev("no-revisions-yet", "foo", 1234),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("no-revisions-yet", "foo", 1234,
				// The following properties are set when we first reconcile a
				// Configuration and a Revision is created.
				WithLatestCreated("no-revisions-yet-01234"), WithConfigObservedGen),
		}, {
			Object: cfg("no-revisions-yet", "foo", 1234,
				// The following properties are set when we first reconcile a
				// Configuration and a Revision is created.
				WithLatestCreated("no-revisions-yet-01234"), WithConfigObservedGen),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Revision %q", "no-revisions-yet-01234"),
		},
		Key: "foo/no-revisions-yet",
	}, {
		Name: "create revision byo name",
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		Objects: []runtime.Object{
			cfg("byo-name-create", "foo", 1234, func(cfg *v1.Configuration) {
				cfg.Spec.GetTemplate().Name = "byo-name-create-foo"
			}),
		},
		WantCreates: []runtime.Object{
			rev("byo-name-create", "foo", 1234, func(rev *v1.Revision) {
				rev.Name = "byo-name-create-foo"
				rev.GenerateName = ""
			}),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("byo-name-create", "foo", 1234, func(cfg *v1.Configuration) {
				cfg.Spec.GetTemplate().Name = "byo-name-create-foo"
			},
				// The following properties are set when we first reconcile a
				// Configuration and a Revision is created.
				WithLatestCreated("byo-name-create-foo"), WithConfigObservedGen),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Revision %q", "byo-name-create-foo"),
		},
		Key: "foo/byo-name-create",
	}, {
		Name: "create revision byo name (exists)",
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		Objects: []runtime.Object{
			cfg("byo-name-exists", "foo", 1234, func(cfg *v1.Configuration) {
				cfg.Spec.GetTemplate().Name = "byo-name-exists-foo"
			},
				// The following properties are set when we first reconcile a
				// Configuration and a Revision is created.
				WithLatestCreated("byo-name-exists-foo"), WithConfigObservedGen),
			rev("byo-name-exists", "foo", 1234, WithCreationTimestamp(now), func(rev *v1.Revision) {
				rev.Name = "byo-name-exists-foo"
				rev.GenerateName = ""
			}),
		},
		Key: "foo/byo-name-exists",
	}, {
		Name: "create revision byo name (exists, wrong generation, right spec)",
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		// This example shows what we might see with a `git revert` in GitOps.
		Objects: []runtime.Object{
			cfg("byo-name-git-revert", "foo", 1234, func(cfg *v1.Configuration) {
				cfg.Spec.GetTemplate().Name = "byo-name-git-revert-foo"
			}),
			rev("byo-name-git-revert", "foo", 1200, WithCreationTimestamp(now), func(rev *v1.Revision) {
				rev.Name = "byo-name-git-revert-foo"
				rev.GenerateName = ""
			}),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("byo-name-git-revert", "foo", 1234, func(cfg *v1.Configuration) {
				cfg.Spec.GetTemplate().Name = "byo-name-git-revert-foo"
			}, WithLatestCreated("byo-name-git-revert-foo"), WithConfigObservedGen),
		}},
		Key: "foo/byo-name-git-revert",
	}, {
		Name: "create revision byo name (exists @ wrong generation w/ wrong spec)",
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		Objects: []runtime.Object{
			cfg("byo-name-wrong-gen-wrong-spec", "foo", 1234, func(cfg *v1.Configuration) {
				cfg.Spec.GetTemplate().Name = "byo-name-wrong-gen-wrong-spec-foo"
			}),
			rev("byo-name-wrong-gen-wrong-spec", "foo", 1200, func(rev *v1.Revision) {
				rev.Name = "byo-name-wrong-gen-wrong-spec-foo"
				rev.GenerateName = ""
				rev.Spec.GetContainer().Env = append(rev.Spec.GetContainer().Env, corev1.EnvVar{
					Name:  "FOO",
					Value: "bar",
				})
			}),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("byo-name-wrong-gen-wrong-spec", "foo", 1234, func(cfg *v1.Configuration) {
				cfg.Spec.GetTemplate().Name = "byo-name-wrong-gen-wrong-spec-foo"
			}, MarkRevisionCreationFailed(`revisions.serving.knative.dev "byo-name-wrong-gen-wrong-spec-foo" already exists`), WithConfigObservedGen),
		}},
		Key: "foo/byo-name-wrong-gen-wrong-spec",
	}, {
		Name: "create revision byo name (exists not owned)",
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		Objects: []runtime.Object{
			cfg("byo-rev-not-owned", "foo", 1234, func(cfg *v1.Configuration) {
				cfg.Spec.GetTemplate().Name = "byo-rev-not-owned-foo"
			}),
			rev("byo-rev-not-owned", "foo", 1200, func(rev *v1.Revision) {
				rev.Name = "byo-rev-not-owned-foo"
				rev.GenerateName = ""
				rev.OwnerReferences = nil
			}),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("byo-rev-not-owned", "foo", 1234, func(cfg *v1.Configuration) {
				cfg.Spec.GetTemplate().Name = "byo-rev-not-owned-foo"
			}, MarkRevisionCreationFailed(`revisions.serving.knative.dev "byo-rev-not-owned-foo" already exists`), WithConfigObservedGen),
		}},
		Key: "foo/byo-rev-not-owned",
	}, {
		Name: "webhook validation failure",
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		// If we attempt to create a Revision with a bad ContainerConcurrency set, we fail.
		WantErr: true,
		Objects: []runtime.Object{
			cfg("validation-failure", "foo", 1234, WithConfigContainerConcurrency(-1)),
		},
		WantCreates: []runtime.Object{
			rev("validation-failure", "foo", 1234, WithRevContainerConcurrency(-1)),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("validation-failure", "foo", 1234, WithConfigContainerConcurrency(-1),
				// Expect Revision creation to fail with the following error.
				MarkRevisionCreationFailed("expected 0 <= -1 <= 1000: spec.containerConcurrency"), WithConfigObservedGen),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "CreationFailed", "Failed to create Revision: expected 0 <= -1 <= 1000: spec.containerConcurrency"),
			Eventf(corev1.EventTypeWarning, "UpdateFailed", `Failed to update status for "validation-failure": expected 0 <= -1 <= 1000: spec.template.spec.containerConcurrency`),
		},
		Key: "foo/validation-failure",
	}, {
		Name: "reconcile revision matching generation (ready: unknown)",
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		Objects: []runtime.Object{
			cfg("matching-revision-not-done", "foo", 5432),
			rev("matching-revision-not-done", "foo", 5432,
				WithCreationTimestamp(now),
				WithRevName("matching-revision-not-done-00001"),
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("matching-revision-not-done", "foo", 5432,
				// If the Revision already exists, we still update these fields.
				// This could happen if the prior status update failed for some reason.
				WithLatestCreated("matching-revision-not-done-00001"), WithConfigObservedGen),
		}},
		Key: "foo/matching-revision-not-done",
	}, {
		Name: "reconcile revision matching generation (ready: true)",
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		Objects: []runtime.Object{
			cfg("matching-revision-done", "foo", 5555, WithLatestCreated("matching-revision-done-00001"), WithConfigObservedGen),
			rev("matching-revision-done", "foo", 5555,
				WithCreationTimestamp(now), MarkRevisionReady, WithRevName("matching-revision-done-00001")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("matching-revision-done", "foo", 5555, WithConfigObservedGen,
				// When we see the LatestCreatedRevision become Ready, then we
				// update the latest ready revision.
				WithLatestCreated("matching-revision-done-00001"),
				WithLatestReady("matching-revision-done-00001")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "ConfigurationReady", "Configuration becomes ready"),
			Eventf(corev1.EventTypeNormal, "LatestReadyUpdate", "LatestReadyRevisionName updated to %q",
				"matching-revision-done-00001"),
		},
		Key: "foo/matching-revision-done",
	}, {
		Name: "reconcile revision matching generation (ready: true, idempotent)",
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		Objects: []runtime.Object{
			cfg("matching-revision-done-idempotent", "foo", 5566,
				WithConfigObservedGen, WithLatestCreated("matching-revision"), WithLatestReady("matching-revision")),
			rev("matching-revision-done-idempotent", "foo", 5566,
				WithCreationTimestamp(now), MarkRevisionReady, WithRevName("matching-revision")),
		},
		Key: "foo/matching-revision-done-idempotent",
	}, {
		Name: "reconcile revision matching generation (ready: false)",
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		Objects: []runtime.Object{
			cfg("matching-revision-failed", "foo", 5555, WithLatestCreated("matching-revision"), WithConfigObservedGen),
			rev("matching-revision-failed", "foo", 5555,
				WithCreationTimestamp(now), MarkContainerMissing, WithRevName("matching-revision")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("matching-revision-failed", "foo", 5555,
				WithLatestCreated("matching-revision"), WithConfigObservedGen,
				// When the LatestCreatedRevision reports back a failure,
				// then we surface that failure.
				MarkLatestCreatedFailed("It's the end of the world as we know it")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "LatestCreatedFailed", "Latest created revision %q has failed",
				"matching-revision"),
		},
		Key: "foo/matching-revision-failed",
	}, {
		Name: "reconcile revision matching generation (ready: bad)",
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		Objects: []runtime.Object{
			cfg("bad-condition", "foo", 5555, WithLatestCreated("bad-condition"), WithConfigObservedGen),
			rev("bad-condition", "foo", 5555,
				WithRevName("bad-condition"),
				WithRevStatus(v1.RevisionStatus{
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{{
							Type:     v1.RevisionConditionReady,
							Status:   "Bad",
							Severity: "Error",
						}},
					},
				})),
		},
		WantErr: true,
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `unrecognized condition status: Bad on revision "bad-condition"`),
		},
		Key: "foo/bad-condition",
	}, {
		Name: "failure creating revision",
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		// We induce a failure creating a revision
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "revisions"),
		},
		Objects: []runtime.Object{
			cfg("create-revision-failure", "foo", 99998),
		},
		WantCreates: []runtime.Object{
			rev("create-revision-failure", "foo", 99998),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("create-revision-failure", "foo", 99998,
				// When we fail to create a Revision is should be surfaced in
				// the Configuration status.
				MarkRevisionCreationFailed("inducing failure for create revisions"), WithConfigObservedGen),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "CreationFailed", "Failed to create Revision: inducing failure for create revisions"),
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to create Revision: inducing failure for create revisions"),
		},
		Key: "foo/create-revision-failure",
	}, {
		Name: "failure updating configuration status",
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		// Induce a failure updating the status of the configuration.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "configurations"),
		},
		Objects: []runtime.Object{
			cfg("update-config-failure", "foo", 1234),
		},
		WantCreates: []runtime.Object{
			rev("update-config-failure", "foo", 1234),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("update-config-failure", "foo", 1234,
				// These would be the status updates after a first
				// reconcile, which we use to trigger the update
				// where we've induced a failure.
				WithLatestCreated("update-config-failure-01234"), WithConfigObservedGen),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Revision %q", "update-config-failure-01234"),
			Eventf(corev1.EventTypeWarning, "UpdateFailed", `Failed to update status for "update-config-failure": inducing failure for update configurations`),
		},
		Key: "foo/update-config-failure",
	}, {
		Name: "failed revision recovers",
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		Objects: []runtime.Object{
			cfg("revision-recovers", "foo", 1337,
				WithLatestCreated("revision-recovers-00001"),
				WithLatestReady("revision-recovers-00001"),
				WithConfigObservedGen,
				MarkLatestCreatedFailed("Weebles wobble, but they don't fall down")),
			rev("revision-recovers", "foo", 1337,
				WithCreationTimestamp(now),
				WithRevName("revision-recovers-00001"),
				MarkRevisionReady,
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("revision-recovers", "foo", 1337,
				WithLatestCreated("revision-recovers-00001"),
				WithLatestReady("revision-recovers-00001"),
				WithConfigObservedGen,
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
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		Objects: []runtime.Object{
			// double-trouble needs to be first for this test to fail
			// when no fix is present
			cfg("double-trouble", "foo", 1,
				WithLatestCreated("double-trouble-00001"),
				WithLatestReady("double-trouble-00001"), WithConfigObservedGen),
			cfg("first-trouble", "foo", 1,
				WithLatestCreated("first-trouble-00001"),
				WithLatestReady("first-trouble-00001"), WithConfigObservedGen),

			rev("first-trouble", "foo", 1,
				WithRevName("first-trouble-00001"),
				WithCreationTimestamp(now), MarkRevisionReady),
			rev("double-trouble", "foo", 1,
				WithRevName("double-trouble-00001"),
				WithCreationTimestamp(now), MarkRevisionReady),
		},
		Key: "foo/double-trouble",
	}, {
		Name: "three revisions with the latest revision failed, the latest ready should be updated to the last ready revision",
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		Objects: []runtime.Object{
			cfg("threerevs", "foo", 3,
				WithLatestCreated("threerevs-00002"),
				WithLatestReady("threerevs-00001"), WithConfigObservedGen, func(cfg *v1.Configuration) {
					cfg.Spec.GetTemplate().Name = "threerevs-00003"
				},
			),
			rev("threerevs", "foo", 1,
				WithRevName("threerevs-00001"),
				WithCreationTimestamp(now), MarkRevisionReady),
			rev("threerevs", "foo", 2,
				WithRevName("threerevs-00002"),
				WithCreationTimestamp(now), MarkRevisionReady),
			rev("threerevs", "foo", 3,
				WithRevName("threerevs-00003"),
				WithCreationTimestamp(now), MarkInactive("", "")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("threerevs", "foo", 3,
				WithLatestCreated("threerevs-00003"),
				WithLatestReady("threerevs-00002"),
				WithConfigObservedGen, func(cfg *v1.Configuration) {
					cfg.Spec.GetTemplate().Name = "threerevs-00003"
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "LatestReadyUpdate", "LatestReadyRevisionName updated to %q", "threerevs-00002"),
		},
		Key: "foo/threerevs",
	}, {
		Name: "revision not ready, the latest ready should be updated, but the configuration should still be ready==Unknown",
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		Objects: []runtime.Object{
			cfg("revnotready", "foo", 3,
				WithLatestCreated("revnotready-00002"),
				WithLatestReady("revnotready-00001"), WithConfigObservedGen, func(cfg *v1.Configuration) {
					cfg.Spec.GetTemplate().Name = "revnotready-00003"
				},
			),
			rev("revnotready", "foo", 1,
				WithRevName("revnotready-00001"),
				WithCreationTimestamp(now), MarkRevisionReady),
			rev("revnotready", "foo", 2,
				WithRevName("revnotready-00002"),
				WithCreationTimestamp(now), MarkRevisionReady),
			rev("revnotready", "foo", 3,
				WithRevName("revnotready-00003"),
				WithCreationTimestamp(now)),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("revnotready", "foo", 3,
				// The config should NOT be ready, because LCR != LRR
				WithLatestCreated("revnotready-00003"),
				WithLatestReady("revnotready-00002"),
				WithConfigObservedGen, func(cfg *v1.Configuration) {
					cfg.Spec.GetTemplate().Name = "revnotready-00003"
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "LatestReadyUpdate", "LatestReadyRevisionName updated to %q", "revnotready-00002"),
		},
		Key: "foo/revnotready",
	}, {
		Name: "current LRR doesn't exist, LCR is ready",
		Ctx:  config.ToContext(context.Background(), config.FromContext(testCtx)),
		Objects: []runtime.Object{
			cfg("lrrnotexist", "foo", 2,
				WithLatestCreated("lrrnotexist-00002"),
				WithLatestReady("lrrnotexist-00001"), WithConfigObservedGen, func(cfg *v1.Configuration) {
					cfg.Spec.GetTemplate().Name = "lrrnotexist-00002"
				},
			),
			rev("lrrnotexist", "foo", 1,
				WithRevName("lrrnotexist-00000"),
				WithCreationTimestamp(now), MarkRevisionReady),
			rev("lrrnotexist", "foo", 2,
				WithRevName("lrrnotexist-00002"),
				WithCreationTimestamp(now), MarkRevisionReady),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("lrrnotexist", "foo", 2,
				WithLatestCreated("lrrnotexist-00002"),
				WithLatestReady("lrrnotexist-00002"),
				WithConfigObservedGen, func(cfg *v1.Configuration) {
					cfg.Spec.GetTemplate().Name = "lrrnotexist-00002"
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "LatestReadyUpdate", "LatestReadyRevisionName updated to %q", "lrrnotexist-00002"),
		},
		Key: "foo/lrrnotexist",
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		retryAttempted = false
		r := &Reconciler{
			client:         servingclient.Get(ctx),
			revisionLister: listers.GetRevisionLister(),
			clock:          testClock,
		}

		return configreconciler.NewReconciler(ctx, logging.FromContext(ctx),
			servingclient.Get(ctx), listers.GetConfigurationLister(),
			controller.GetEventRecorder(ctx), r)

	}))
}

func cfg(name, namespace string, generation int64, co ...ConfigOption) *v1.Configuration {
	c := &v1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: generation,
		},
		Spec: v1.ConfigurationSpec{
			Template: v1.RevisionTemplateSpec{
				Spec: *revisionSpec.DeepCopy(),
			},
		},
	}
	for _, opt := range co {
		opt(c)
	}
	c.SetDefaults(context.Background())
	return c
}

func rev(name, namespace string, generation int64, ro ...RevisionOption) *v1.Revision {
	r := resources.MakeRevision(testCtx, cfg(name, namespace, generation), testClock)
	r.SetDefaults(context.Background())
	for _, opt := range ro {
		opt(r)
	}
	return r
}
