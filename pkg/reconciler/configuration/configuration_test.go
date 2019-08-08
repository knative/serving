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
	"testing"
	"time"

	// Inject the fake informers we need.
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/configuration/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision/fake"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"
	duckv1beta1 "knative.dev/pkg/apis/duck/v1beta1"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/configuration/resources"

	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1alpha1"
	. "knative.dev/serving/pkg/testing/v1alpha1"
)

var revisionSpec = v1alpha1.RevisionSpec{
	RevisionSpec: v1beta1.RevisionSpec{
		PodSpec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Image: "busybox",
			}},
		},
		TimeoutSeconds: ptr.Int64(60),
	},
}

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
		Name: "nop deletion reconcile",
		// Test that with a DeletionTimestamp we do nothing.
		Objects: []runtime.Object{
			cfg("foo", "delete-pending", 1234, WithConfigDeletionTimestamp),
		},
		Key: "foo/delete-pending",
	}, {
		Name: "create revision matching generation",
		Objects: []runtime.Object{
			cfg("no-revisions-yet", "foo", 1234),
		},
		WantCreates: []runtime.Object{
			rev("no-revisions-yet", "foo", 1234),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("no-revisions-yet", "foo", 1234,
				// The following properties are set when we first reconcile a
				// Configuration and a Revision is created.
				WithLatestCreated("no-revisions-yet-00001"), WithObservedGen),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Revision %q", "no-revisions-yet-00001"),
		},
		Key: "foo/no-revisions-yet",
	}, {
		Name: "create revision byo name",
		Objects: []runtime.Object{
			cfg("byo-name-create", "foo", 1234, func(cfg *v1alpha1.Configuration) {
				cfg.Spec.GetTemplate().Name = "byo-name-create-foo"
			}),
		},
		WantCreates: []runtime.Object{
			rev("byo-name-create", "foo", 1234, func(rev *v1alpha1.Revision) {
				rev.Name = "byo-name-create-foo"
				rev.GenerateName = ""
			}),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("byo-name-create", "foo", 1234, func(cfg *v1alpha1.Configuration) {
				cfg.Spec.GetTemplate().Name = "byo-name-create-foo"
			},
				// The following properties are set when we first reconcile a
				// Configuration and a Revision is created.
				WithLatestCreated("byo-name-create-foo"), WithObservedGen),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Revision %q", "byo-name-create-foo"),
		},
		Key: "foo/byo-name-create",
	}, {
		Name: "create revision byo name (exists)",
		Objects: []runtime.Object{
			cfg("byo-name-exists", "foo", 1234, func(cfg *v1alpha1.Configuration) {
				cfg.Spec.GetTemplate().Name = "byo-name-exists-foo"
			},
				// The following properties are set when we first reconcile a
				// Configuration and a Revision is created.
				WithLatestCreated("byo-name-exists-foo"), WithObservedGen),
			rev("byo-name-exists", "foo", 1234, WithCreationTimestamp(now), func(rev *v1alpha1.Revision) {
				rev.Name = "byo-name-exists-foo"
				rev.GenerateName = ""
			}),
		},
		Key: "foo/byo-name-exists",
	}, {
		Name: "create revision byo name (exists, wrong generation, right spec)",
		// This example shows what we might see with a `git revert` in GitOps.
		Objects: []runtime.Object{
			cfg("byo-name-git-revert", "foo", 1234, func(cfg *v1alpha1.Configuration) {
				cfg.Spec.GetTemplate().Name = "byo-name-git-revert-foo"
			}),
			rev("byo-name-git-revert", "foo", 1200, WithCreationTimestamp(now), func(rev *v1alpha1.Revision) {
				rev.Name = "byo-name-git-revert-foo"
				rev.GenerateName = ""
			}),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("byo-name-git-revert", "foo", 1234, func(cfg *v1alpha1.Configuration) {
				cfg.Spec.GetTemplate().Name = "byo-name-git-revert-foo"
			}, WithLatestCreated("byo-name-git-revert-foo"), WithObservedGen),
		}},
		Key: "foo/byo-name-git-revert",
	}, {
		Name: "create revision byo name (exists @ wrong generation w/ wrong spec)",
		Objects: []runtime.Object{
			cfg("byo-name-wrong-gen-wrong-spec", "foo", 1234, func(cfg *v1alpha1.Configuration) {
				cfg.Spec.GetTemplate().Name = "byo-name-wrong-gen-wrong-spec-foo"
			}),
			rev("byo-name-wrong-gen-wrong-spec", "foo", 1200, func(rev *v1alpha1.Revision) {
				rev.Name = "byo-name-wrong-gen-wrong-spec-foo"
				rev.GenerateName = ""
				rev.Spec.GetContainer().Env = append(rev.Spec.GetContainer().Env, corev1.EnvVar{
					Name:  "FOO",
					Value: "bar",
				})
			}),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("byo-name-wrong-gen-wrong-spec", "foo", 1234, func(cfg *v1alpha1.Configuration) {
				cfg.Spec.GetTemplate().Name = "byo-name-wrong-gen-wrong-spec-foo"
			}, MarkRevisionCreationFailed(`revisions.serving.knative.dev "byo-name-wrong-gen-wrong-spec-foo" already exists`)),
		}},
		Key: "foo/byo-name-wrong-gen-wrong-spec",
	}, {
		Name: "create revision byo name (exists not owned)",
		Objects: []runtime.Object{
			cfg("byo-rev-not-owned", "foo", 1234, func(cfg *v1alpha1.Configuration) {
				cfg.Spec.GetTemplate().Name = "byo-rev-not-owned-foo"
			}),
			rev("byo-rev-not-owned", "foo", 1200, func(rev *v1alpha1.Revision) {
				rev.Name = "byo-rev-not-owned-foo"
				rev.GenerateName = ""
				rev.OwnerReferences = nil
			}),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("byo-rev-not-owned", "foo", 1234, func(cfg *v1alpha1.Configuration) {
				cfg.Spec.GetTemplate().Name = "byo-rev-not-owned-foo"
			}, MarkRevisionCreationFailed(`revisions.serving.knative.dev "byo-rev-not-owned-foo" already exists`)),
		}},
		Key: "foo/byo-rev-not-owned",
	}, {
		Name: "webhook validation failure",
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
				MarkRevisionCreationFailed("expected 0 <= -1 <= 1000: spec.containerConcurrency")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "CreationFailed", "Failed to create Revision for Configuration %q: %v",
				"validation-failure", "expected 0 <= -1 <= 1000: spec.containerConcurrency"),
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "Failed to update status for Configuration %q: %v",
				"validation-failure", "expected 0 <= -1 <= 1000: spec.template.spec.containerConcurrency"),
		},
		Key: "foo/validation-failure",
	}, {
		Name: "reconcile revision matching generation (ready: unknown)",
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
				WithLatestCreated("matching-revision-not-done-00001"), WithObservedGen),
		}},
		Key: "foo/matching-revision-not-done",
	}, {
		Name: "reconcile revision matching generation (ready: true)",
		Objects: []runtime.Object{
			cfg("matching-revision-done", "foo", 5555, WithLatestCreated("matching-revision-done-00001"), WithObservedGen),
			rev("matching-revision-done", "foo", 5555,
				WithCreationTimestamp(now), MarkRevisionReady, WithRevName("matching-revision-done-00001")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("matching-revision-done", "foo", 5555, WithObservedGen,
				// When we see the LatestCreatedRevision become Ready, then we
				// update the latest ready revision.
				WithLatestReady("matching-revision-done-00001"),
				WithLatestCreated("matching-revision-done-00001")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "ConfigurationReady", "Configuration becomes ready"),
			Eventf(corev1.EventTypeNormal, "LatestReadyUpdate", "LatestReadyRevisionName updated to %q",
				"matching-revision-done-00001"),
		},
		Key: "foo/matching-revision-done",
	}, {
		Name: "reconcile revision matching generation (ready: true, idempotent)",
		Objects: []runtime.Object{
			cfg("matching-revision-done-idempotent", "foo", 5566,
				WithObservedGen, WithLatestCreated("matching-revision"), WithLatestReady("matching-revision")),
			rev("matching-revision-done-idempotent", "foo", 5566,
				WithCreationTimestamp(now), MarkRevisionReady, WithRevName("matching-revision")),
		},
		Key: "foo/matching-revision-done-idempotent",
	}, {
		Name: "reconcile revision matching generation (ready: false)",
		Objects: []runtime.Object{
			cfg("matching-revision-failed", "foo", 5555, WithLatestCreated("matching-revision"), WithObservedGen),
			rev("matching-revision-failed", "foo", 5555,
				WithCreationTimestamp(now), MarkContainerMissing, WithRevName("matching-revision")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("matching-revision-failed", "foo", 5555,
				WithLatestCreated("matching-revision"), WithObservedGen,
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
		Objects: []runtime.Object{
			cfg("bad-condition", "foo", 5555, WithLatestCreated("bad-condition"), WithObservedGen),
			rev("bad-condition", "foo", 5555,
				WithRevName("bad-condition"),
				WithRevStatus(v1alpha1.RevisionStatus{
					Status: duckv1beta1.Status{
						Conditions: duckv1beta1.Conditions{{
							Type:     v1alpha1.RevisionConditionReady,
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
				MarkRevisionCreationFailed("inducing failure for create revisions")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "CreationFailed", "Failed to create Revision for Configuration %q: %v",
				"create-revision-failure", "inducing failure for create revisions"),
			Eventf(corev1.EventTypeWarning, "InternalError", "inducing failure for create revisions"),
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
		WantCreates: []runtime.Object{
			rev("update-config-failure", "foo", 1234),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfg("update-config-failure", "foo", 1234,
				// These would be the status updates after a first
				// reconcile, which we use to trigger the update
				// where we've induced a failure.
				WithLatestCreated("update-config-failure-00001"), WithObservedGen),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Revision %q", "update-config-failure-00001"),
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "Failed to update status for Configuration %q: %v",
				"update-config-failure", "inducing failure for update configurations"),
		},
		Key: "foo/update-config-failure",
	}, {
		Name: "failed revision recovers",
		Objects: []runtime.Object{
			cfg("revision-recovers", "foo", 1337,
				WithLatestCreated("revision-recovers-00001"),
				WithLatestReady("revision-recovers-00001"),
				WithObservedGen,
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
				WithObservedGen,
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
				WithLatestCreated("double-trouble-00001"),
				WithLatestReady("double-trouble-00001"), WithObservedGen),
			cfg("first-trouble", "foo", 1,
				WithLatestCreated("first-trouble-00001"),
				WithLatestReady("first-trouble-00001"), WithObservedGen),

			rev("first-trouble", "foo", 1,
				WithRevName("first-trouble-00001"),
				WithCreationTimestamp(now), MarkRevisionReady),
			rev("double-trouble", "foo", 1,
				WithRevName("double-trouble-00001"),
				WithCreationTimestamp(now), MarkRevisionReady),
		},
		Key: "foo/double-trouble",
	}}

	defer logtesting.ClearAll()
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		return &Reconciler{
			Base:                reconciler.NewBase(ctx, controllerAgentName, cmw),
			configurationLister: listers.GetConfigurationLister(),
			revisionLister:      listers.GetRevisionLister(),
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
			Template: &v1alpha1.RevisionTemplateSpec{
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

func rev(name, namespace string, generation int64, ro ...RevisionOption) *v1alpha1.Revision {
	r := resources.MakeRevision(cfg(name, namespace, generation))
	r.SetDefaults(v1beta1.WithUpgradeViaDefaulting(context.Background()))
	for _, opt := range ro {
		opt(r)
	}
	return r
}
