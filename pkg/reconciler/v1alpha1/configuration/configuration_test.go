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
	"testing"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/configuration/resources"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"

	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
)

var (
	boolTrue     = true
	revisionSpec = v1alpha1.RevisionSpec{
		Container: corev1.Container{
			Image: "busybox",
		},
	}
	buildSpec = buildv1alpha1.BuildSpec{
		Steps: []corev1.Container{{
			Image: "build-step1",
		}, {
			Image: "build-step2",
		}},
	}
)

// This is heavily based on the way the OpenShift Ingress controller tests its reconciliation method.
func TestReconcile(t *testing.T) {
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
			resources.MakeRevision(cfg("no-revisions-yet", "foo", 1234)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfgWithStatus("no-revisions-yet", "foo", 1234,
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "no-revisions-yet-01234",
					ObservedGeneration:        1234,
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				}),
		}},
		Key: "foo/no-revisions-yet",
	}, {
		Name: "webhook validation failure",
		// If we attempt to create a Revision with a bad ConcurrencyModel set, we fail.
		WantErr: true,
		Objects: []runtime.Object{
			setConcurrencyModel(cfg("validation-failure", "foo", 1234), "Bogus"),
		},
		WantCreates: []metav1.Object{
			setRevConcurrencyModel(resources.MakeRevision(cfg("validation-failure", "foo", 1234)), "Bogus"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: setConcurrencyModel(cfgWithStatus("validation-failure", "foo", 1234,
				v1alpha1.ConfigurationStatus{
					Conditions: duckv1alpha1.Conditions{{
						Type:    v1alpha1.ConfigurationConditionReady,
						Status:  corev1.ConditionFalse,
						Reason:  "RevisionFailed",
						Message: `Revision creation failed with message: "invalid value \"Bogus\": spec.concurrencyModel".`,
					}},
				}), "Bogus"),
		}},
		Key: "foo/validation-failure",
	}, {
		Name: "create revision matching generation with build",
		Objects: []runtime.Object{
			cfgWithBuild("need-rev-and-build", "foo", 99998, &buildSpec),
		},
		WantCreates: []metav1.Object{
			resources.MakeBuild(cfgWithBuild("need-rev-and-build", "foo", 99998, &buildSpec)),
			resources.MakeRevision(cfgWithBuild("need-rev-and-build", "foo", 99998, &buildSpec)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfgWithBuildAndStatus("need-rev-and-build", "foo", 99998, &buildSpec,
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "need-rev-and-build-99998",
					ObservedGeneration:        99998,
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			),
		}},
		Key: "foo/need-rev-and-build",
	}, {
		Name: "reconcile revision matching generation (ready: unknown)",
		Objects: []runtime.Object{
			cfg("matching-revision-not-done", "foo", 5432),
			resources.MakeRevision(cfg("matching-revision-not-done", "foo", 5432)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfgWithStatus("matching-revision-not-done", "foo", 5432,
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "matching-revision-not-done-05432",
					ObservedGeneration:        5432,
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			),
		}},
		Key: "foo/matching-revision-not-done",
	}, {
		Name: "reconcile revision matching generation (ready: true)",
		Objects: []runtime.Object{
			cfg("matching-revision-done", "foo", 5555),
			makeRevReady(t, resources.MakeRevision(cfg("matching-revision-done", "foo", 5555))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfgWithStatus("matching-revision-done", "foo", 5555,
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "matching-revision-done-05555",
					LatestReadyRevisionName:   "matching-revision-done-05555",
					ObservedGeneration:        5555,
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionTrue,
					}},
				},
			),
		}},
		Key: "foo/matching-revision-done",
	}, {
		Name: "reconcile revision matching generation (ready: true, idempotent)",
		Objects: []runtime.Object{
			cfgWithStatus("matching-revision-done-idempotent", "foo", 5566,
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "matching-revision-done-idempotent-05566",
					LatestReadyRevisionName:   "matching-revision-done-idempotent-05566",
					ObservedGeneration:        5566,
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionTrue,
					}},
				},
			),
			makeRevReady(t, resources.MakeRevision(cfg("matching-revision-done-idempotent", "foo", 5566))),
		},
		Key: "foo/matching-revision-done-idempotent",
	}, {
		Name: "reconcile revision matching generation (ready: false)",
		Objects: []runtime.Object{
			cfg("matching-revision-failed", "foo", 5555),
			makeRevFailed(resources.MakeRevision(cfg("matching-revision-failed", "foo", 5555))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfgWithStatus("matching-revision-failed", "foo", 5555,
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "matching-revision-failed-05555",
					ObservedGeneration:        5555,
					Conditions: duckv1alpha1.Conditions{{
						Type:    v1alpha1.ConfigurationConditionReady,
						Status:  corev1.ConditionFalse,
						Reason:  "RevisionFailed",
						Message: `Revision "matching-revision-failed-05555" failed with message: "It's the end of the world as we know it".`,
					}},
				},
			),
		}},
		Key: "foo/matching-revision-failed",
	}, {
		Name: "reconcile revision matching generation (ready: bad)",
		Objects: []runtime.Object{
			cfg("bad-condition", "foo", 5555),
			makeRevStatus(resources.MakeRevision(cfg("bad-condition", "foo", 5555)),
				v1alpha1.RevisionStatus{
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.RevisionConditionReady,
						Status: "Bad",
					}},
				},
			),
		},
		WantErr: true,
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfgWithStatus("bad-condition", "foo", 5555,
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "bad-condition-05555",
					ObservedGeneration:        5555,
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			),
		}},
		Key: "foo/bad-condition",
	}, {
		Name: "failure creating build",
		// We induce a failure creating a build
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "builds"),
		},
		Objects: []runtime.Object{
			cfgWithBuild("create-build-failure", "foo", 99998, &buildSpec),
		},
		WantCreates: []metav1.Object{
			resources.MakeBuild(cfgWithBuild("create-build-failure", "foo", 99998, &buildSpec)),
			// No Revision gets created.
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfgWithBuildAndStatus("create-build-failure", "foo", 99998, &buildSpec,
				v1alpha1.ConfigurationStatus{
					Conditions: duckv1alpha1.Conditions{{
						Type:    v1alpha1.ConfigurationConditionReady,
						Status:  corev1.ConditionFalse,
						Reason:  "RevisionFailed",
						Message: `Revision creation failed with message: "inducing failure for create builds".`,
					}},
				},
			),
		}},
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
			resources.MakeRevision(cfg("create-revision-failure", "foo", 99998)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfgWithStatus("create-revision-failure", "foo", 99998,
				v1alpha1.ConfigurationStatus{
					Conditions: duckv1alpha1.Conditions{{
						Type:    v1alpha1.ConfigurationConditionReady,
						Status:  corev1.ConditionFalse,
						Reason:  "RevisionFailed",
						Message: `Revision creation failed with message: "inducing failure for create revisions".`,
					}},
				},
			),
		}},
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
			resources.MakeRevision(cfg("update-config-failure", "foo", 1234)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfgWithStatus("update-config-failure", "foo", 1234,
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "update-config-failure-01234",
					ObservedGeneration:        1234,
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			),
		}},
		Key: "foo/update-config-failure",
	}, {
		Name: "failed revision recovers",
		Objects: []runtime.Object{
			cfgWithStatus("revision-recovers", "foo", 1337,
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "revision-recovers-01337",
					LatestReadyRevisionName:   "revision-recovers-01337",
					Conditions: duckv1alpha1.Conditions{{
						Type:    v1alpha1.ConfigurationConditionReady,
						Status:  corev1.ConditionFalse,
						Reason:  "RevisionFailed",
						Message: `Revision "revision-recovers-01337" failed with message: "Weebles wobble, but they don't fall down".`,
					}},
				},
			),
			makeRevReady(t, resources.MakeRevision(cfg("revision-recovers", "foo", 1337))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: cfgWithStatus("revision-recovers", "foo", 1337,
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "revision-recovers-01337",
					LatestReadyRevisionName:   "revision-recovers-01337",
					ObservedGeneration:        1337,
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionTrue,
					}},
				},
			),
		}},
		Key: "foo/revision-recovers",
	}}

	table.Test(t, MakeFactory(func(listers *Listers, opt reconciler.Options) controller.Reconciler {
		return &Reconciler{
			Base:                reconciler.NewBase(opt, controllerAgentName),
			configurationLister: listers.GetConfigurationLister(),
			revisionLister:      listers.GetRevisionLister(),
		}
	}))
}

func cfgWithBuildAndStatus(name, namespace string, generation int64, build *buildv1alpha1.BuildSpec, status v1alpha1.ConfigurationStatus) *v1alpha1.Configuration {
	var bld *v1alpha1.RawExtension
	if build != nil {
		bld = &v1alpha1.RawExtension{BuildSpec: build}
	}
	return &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.ConfigurationSpec{
			Generation: generation,
			Build:      bld,
			RevisionTemplate: v1alpha1.RevisionTemplateSpec{
				Spec: revisionSpec,
			},
		},
		Status: status,
	}
}

func cfgWithStatus(name, namespace string, generation int64, status v1alpha1.ConfigurationStatus) *v1alpha1.Configuration {
	return cfgWithBuildAndStatus(name, namespace, generation, nil, status)
}

func cfgWithBuild(name, namespace string, generation int64, build *buildv1alpha1.BuildSpec) *v1alpha1.Configuration {
	return cfgWithBuildAndStatus(name, namespace, generation, build, v1alpha1.ConfigurationStatus{})
}

func cfg(name, namespace string, generation int64) *v1alpha1.Configuration {
	return cfgWithStatus(name, namespace, generation, v1alpha1.ConfigurationStatus{})
}

func setConcurrencyModel(cfg *v1alpha1.Configuration, ss v1alpha1.RevisionRequestConcurrencyModelType) *v1alpha1.Configuration {
	cfg.Spec.RevisionTemplate.Spec.ConcurrencyModel = ss
	return cfg
}

func setRevConcurrencyModel(rev *v1alpha1.Revision, ss v1alpha1.RevisionRequestConcurrencyModelType) *v1alpha1.Revision {
	rev.Spec.ConcurrencyModel = ss
	return rev
}

func makeRevReady(t *testing.T, rev *v1alpha1.Revision) *v1alpha1.Revision {
	rev.Status.InitializeConditions()
	rev.Status.MarkContainerHealthy()
	rev.Status.MarkResourcesAvailable()
	rev.Status.MarkActive()
	if !rev.Status.IsReady() {
		t.Fatalf("Wanted ready revision: %v", rev)
	}
	return rev
}

func makeRevFailed(rev *v1alpha1.Revision) *v1alpha1.Revision {
	rev.Status.InitializeConditions()
	rev.Status.MarkContainerMissing("It's the end of the world as we know it")
	return rev
}

func makeRevStatus(rev *v1alpha1.Revision, status v1alpha1.RevisionStatus) *v1alpha1.Revision {
	rev.Status = status
	return rev
}
