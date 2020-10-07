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

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	// Install our fake informers
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/route/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/service/fake"

	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	cfgmap "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	autoscalercfg "knative.dev/serving/pkg/autoscaler/config"
	servingclient "knative.dev/serving/pkg/client/injection/client"
	ksvcreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/service"
	configresources "knative.dev/serving/pkg/reconciler/configuration/resources"
	"knative.dev/serving/pkg/reconciler/service/resources"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/clock"
	clientgotesting "k8s.io/client-go/testing"

	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1"
	. "knative.dev/serving/pkg/testing/v1"
)

func TestReconcile(t *testing.T) {
	retryAttempted := false
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
			DefaultService("delete-pending", "foo", WithServiceDeletionTimestamp),
		},
		Key: "foo/delete-pending",
	}, {
		Name: "inline - byo rev name used in traffic serialize, with retry",
		Objects: []runtime.Object{
			DefaultService("byo-rev", "foo", WithNamedRevision, WithServiceGeneration(1)),
			config("byo-rev", "foo",
				WithNamedRevision,
				WithConfigGeneration(2)),
		},
		// Route should not be created until config progresses
		WantCreates: []runtime.Object{},
		Key:         "foo/byo-rev",
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("byo-rev", "foo", WithNamedRevision,
				// Route conditions should be at init state while Config should be OutOfDate
				WithInitSvcConditions, WithOutOfDateConfig,
				WithServiceGeneration(1), WithServiceObservedGeneration),
		}, {
			Object: DefaultService("byo-rev", "foo", WithNamedRevision,
				// Route conditions should be at init state while Config should be OutOfDate
				WithInitSvcConditions, WithOutOfDateConfig,
				WithServiceGeneration(1), WithServiceObservedGeneration),
		}},
		WithReactors: []clientgotesting.ReactionFunc{
			func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
				if retryAttempted || !action.Matches("update", "services") || action.GetSubresource() != "status" {
					return false, nil, nil
				}
				retryAttempted = true
				return true, nil, apierrs.NewConflict(v1.Resource("foo"), "bar", errors.New("foo"))
			},
		},
	}, {
		Name: "inline - byo rev name - existing revision - same spec",
		Objects: []runtime.Object{
			DefaultService("byo-rev", "foo", WithNamedRevision, WithServiceGeneration(2)),
			config("byo-rev", "foo", WithNamedRevision,
				WithConfigGeneration(2), WithConfigObservedGen),
			route("byo-rev", "foo", WithNamedRevision,
				WithRouteGeneration(2), WithRouteObservedGeneration),
			&v1.Revision{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "byo-rev-byo",
					Namespace: "foo",
					OwnerReferences: []metav1.OwnerReference{
						*metav1.NewControllerRef(
							config("byo-rev", "foo", WithNamedRevision),
							v1.SchemeGroupVersion.WithKind("Configuration"),
						),
					},
					Labels: map[string]string{
						serving.ConfigurationGenerationLabelKey: "2",
					},
				},
			},
		},
		Key: "foo/byo-rev",
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("byo-rev", "foo", WithNamedRevision,
				// The first reconciliation will initialize the status conditions.
				WithInitSvcConditions, WithServiceObservedGenFailure,
				WithServiceGeneration(2), WithServiceObservedGeneration),
		}},
	}, {
		Name: "inline - byo rev name - existing older revision with same spec",
		Objects: []runtime.Object{
			DefaultService("byo-rev", "foo", WithNamedRevision, WithServiceGeneration(2)),
			config("byo-rev", "foo", WithNamedRevision,
				WithConfigGeneration(2), WithConfigObservedGen),
			route("byo-rev", "foo", WithNamedRevision,
				WithRouteGeneration(2), WithRouteObservedGeneration),
			rev("byo-rev", "foo", WithNamedRevision,
				// Older Revision
				WithConfigGeneration(1), WithConfigObservedGen),
		},
		Key: "foo/byo-rev",
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("byo-rev", "foo",
				WithNamedRevision, WithInitSvcConditions, WithServiceObservedGenFailure,
				WithServiceGeneration(2), WithServiceObservedGeneration),
		}},
	}, {
		Name: "inline - byo rev name - existing older revision with different spec",
		Objects: []runtime.Object{
			DefaultService("byo-rev", "foo", WithNamedRevision, WithServiceGeneration(2)),
			config("byo-rev", "foo", WithNamedRevision,
				WithConfigGeneration(2), WithConfigObservedGen),
			route("byo-rev", "foo", WithNamedRevision,
				WithRouteGeneration(2), WithRouteObservedGeneration),

			&v1.Revision{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "byo-rev-byo",
					Namespace: "foo",
					OwnerReferences: []metav1.OwnerReference{
						*metav1.NewControllerRef(
							config("byo-rev", "foo", WithNamedRevision),
							v1.SchemeGroupVersion.WithKind("Configuration"),
						),
					},
					Labels: map[string]string{
						serving.ConfigurationGenerationLabelKey: "1",
					},
				},
			},
		},
		Key: "foo/byo-rev",
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("byo-rev", "foo", WithNamedRevision,
				WithInitSvcConditions, MarkRevisionNameTaken,
				WithServiceGeneration(2), WithServiceObservedGeneration,
			),
		}},
	}, {
		Name: "inline - byo rev name used in traffic",
		Objects: []runtime.Object{
			DefaultService("byo-rev", "foo", WithNamedRevision, WithServiceGeneration(1)),
		},
		Key: "foo/byo-rev",
		WantCreates: []runtime.Object{
			config("byo-rev", "foo", WithNamedRevision),
			route("byo-rev", "foo", WithNamedRevision),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("byo-rev", "foo", WithNamedRevision,
				// The first reconciliation will initialize the status conditions.
				WithInitSvcConditions, WithServiceObservedGenFailure,
				WithServiceGeneration(1), WithServiceObservedGeneration),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Configuration %q", "byo-rev"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Route %q", "byo-rev"),
		},
	}, {
		Name: "create route and configuration",
		Objects: []runtime.Object{
			DefaultService("run-latest", "foo", WithRunLatestRollout, WithServiceGeneration(1)),
		},
		Key: "foo/run-latest",
		WantCreates: []runtime.Object{
			config("run-latest", "foo", WithRunLatestRollout),
			route("run-latest", "foo", WithRunLatestRollout),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("run-latest", "foo", WithRunLatestRollout,
				// The first reconciliation will initialize the status conditions.
				WithInitSvcConditions, WithServiceObservedGenFailure,
				WithServiceGeneration(1), WithServiceObservedGeneration),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Configuration %q", "run-latest"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Route %q", "run-latest"),
		},
	}, {
		Name: "steady state",
		Objects: []runtime.Object{
			DefaultService("no-updates", "foo", WithRunLatestRollout, WithInitSvcConditions,
				WithServiceGeneration(1), WithServiceObservedGeneration),
			route("no-updates", "foo", WithRunLatestRollout),
			config("no-updates", "foo", WithRunLatestRollout),
		},
		Key: "foo/no-updates",
	}, {
		Name: "update annotations",
		Objects: []runtime.Object{
			DefaultService("update-annos", "foo", WithRunLatestRollout, WithInitSvcConditions,
				func(s *v1.Service) {
					s.Annotations = kmeta.UnionMaps(s.Annotations,
						map[string]string{"new-key": "new-value"})
				}),
			config("update-annos", "foo", WithRunLatestRollout),
			route("update-annos", "foo", WithRunLatestRollout),
		},
		Key: "foo/update-annos",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: config("update-annos", "foo", WithRunLatestRollout,
				func(s *v1.Configuration) {
					s.Annotations = kmeta.UnionMaps(s.Annotations,
						map[string]string{"new-key": "new-value"})
				}),
		}, {
			Object: route("update-annos", "foo", WithRunLatestRollout,
				func(s *v1.Route) {
					s.Annotations = kmeta.UnionMaps(s.Annotations,
						map[string]string{"new-key": "new-value"})
				}),
		}},
	}, {
		Name: "delete annotations",
		Objects: []runtime.Object{
			DefaultService("update-annos", "foo", WithRunLatestRollout, WithInitSvcConditions),
			config("update-annos", "foo", WithRunLatestRollout,
				func(s *v1.Configuration) {
					s.Annotations = kmeta.UnionMaps(s.Annotations,
						map[string]string{"new-key": "new-value"})
				}),
			route("update-annos", "foo", WithRunLatestRollout,
				func(s *v1.Route) {
					s.Annotations = kmeta.UnionMaps(s.Annotations,
						map[string]string{"new-key": "new-value"})
				}),
		},
		Key: "foo/update-annos",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: config("update-annos", "foo", WithRunLatestRollout),
		}, {
			Object: route("update-annos", "foo", WithRunLatestRollout),
		}},
	}, {
		Name: "update route and configuration",
		Objects: []runtime.Object{
			DefaultService("update-route-and-config", "foo", WithRunLatestRollout,
				WithInitSvcConditions),
			// Mutate the Config/Route to have a different body than we want.
			config("update-route-and-config", "foo", WithRunLatestRollout,
				// This is just an unexpected mutation of the config spec vs. the service spec.
				WithConfigContainerConcurrency(5)),
			route("update-route-and-config", "foo", WithRunLatestRollout, MutateRoute),
		},
		Key: "foo/update-route-and-config",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: config("update-route-and-config", "foo", WithRunLatestRollout),
		}, {
			Object: route("update-route-and-config", "foo", WithRunLatestRollout),
		}},
	}, {
		Name: "update route and configuration (bad existing revision)",
		Objects: []runtime.Object{
			DefaultService("update-route-and-config", "foo", WithRunLatestRollout, func(svc *v1.Service) {
				svc.Spec.GetTemplate().Name = "update-route-and-config-blah"
			}, WithInitSvcConditions, WithServiceGeneration(1)),
			// Mutate the Config/Route to have a different body than we want.
			config("update-route-and-config", "foo", WithRunLatestRollout,
				// Change the concurrency to ensure it is corrected.
				WithConfigContainerConcurrency(5)),
			route("update-route-and-config", "foo", WithRunLatestRollout, MutateRoute),
			&v1.Revision{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "update-route-and-config-blah",
					Namespace: "foo",
					// Not labeled with the configuration or the right generation.
				},
			},
		},
		Key: "foo/update-route-and-config",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: config("update-route-and-config", "foo", WithRunLatestRollout,
				func(cfg *v1.Configuration) {
					cfg.Spec.GetTemplate().Name = "update-route-and-config-blah"
				}),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("update-route-and-config", "foo", WithRunLatestRollout, func(svc *v1.Service) {
				svc.Spec.GetTemplate().Name = "update-route-and-config-blah"
			}, WithInitSvcConditions, WithServiceGeneration(1), WithServiceObservedGeneration,
				func(svc *v1.Service) {
					svc.Status.MarkRevisionNameTaken("update-route-and-config-blah")
				}),
		}},
	}, {
		Name: "update route and configuration labels",
		Objects: []runtime.Object{
			// Mutate the Service to add some more labels
			DefaultService("update-route-and-config-labels", "foo", WithRunLatestRollout,
				WithInitSvcConditions, WithServiceLabel("new-label", "new-value")),
			config("update-route-and-config-labels", "foo", WithRunLatestRollout),
			route("update-route-and-config-labels", "foo", WithRunLatestRollout),
		},
		Key: "foo/update-route-and-config-labels",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: config("update-route-and-config-labels", "foo", WithRunLatestRollout, WithConfigLabel("new-label", "new-value")),
		}, {
			Object: route("update-route-and-config-labels", "foo", WithRunLatestRollout, WithRouteLabel(map[string]string{"new-label": "new-value",
				"serving.knative.dev/service": "update-route-and-config-labels"})),
		}},
	}, {
		Name: "update route config labels ignoring serving.knative.dev/route",
		Objects: []runtime.Object{
			// Mutate the Service to add some more labels
			DefaultService("update-child-labels-ignore-route-label", "foo",
				WithRunLatestRollout, WithInitSvcConditions, WithServiceLabel("new-label", "new-value")),
			config("update-child-labels-ignore-route-label", "foo", WithRunLatestRollout),
			route("update-child-labels-ignore-route-label", "foo", WithRunLatestRollout),
		},
		Key: "foo/update-child-labels-ignore-route-label",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: config("update-child-labels-ignore-route-label", "foo", WithRunLatestRollout, WithConfigLabel("new-label", "new-value")),
		}, {
			Object: route("update-child-labels-ignore-route-label", "foo", WithRunLatestRollout, WithRouteLabel(map[string]string{"new-label": "new-value",
				"serving.knative.dev/service": "update-child-labels-ignore-route-label"})),
		}},
	}, {
		Name: "bad configuration update",
		Objects: []runtime.Object{
			// There is no spec.{runLatest,pinned} in this Service, which triggers the error
			// path updating Configuration.
			DefaultService("bad-config-update", "foo", WithInitSvcConditions, WithRunLatestRollout,
				func(svc *v1.Service) {
					svc.Spec.GetTemplate().Spec.GetContainer().Image = "#"
				}),
			config("bad-config-update", "foo", WithRunLatestRollout),
			route("bad-config-update", "foo", WithRunLatestRollout),
		},
		Key:     "foo/bad-config-update",
		WantErr: true,
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: config("bad-config-update", "foo", WithRunLatestRollout,
				func(cfg *v1.Configuration) {
					cfg.Spec.GetTemplate().Spec.GetContainer().Image = "#"
				}),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				"failed to reconcile Configuration: Failed to parse image reference: spec.template.spec.containers[0].image\nimage: \"#\", error: could not parse reference: #"),
		},
	}, {
		Name: "route creation failure",
		// Induce a failure during route creation
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "routes"),
		},
		Objects: []runtime.Object{
			DefaultService("create-route-failure", "foo", WithRunLatestRollout, WithServiceGeneration(1)),
		},
		Key: "foo/create-route-failure",
		WantCreates: []runtime.Object{
			config("create-route-failure", "foo", WithRunLatestRollout),
			route("create-route-failure", "foo", WithRunLatestRollout),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("create-route-failure", "foo", WithRunLatestRollout,
				// First reconcile initializes conditions.
				WithInitSvcConditions, WithServiceObservedGenFailure,
				WithServiceGeneration(1), WithServiceObservedGeneration),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Configuration %q", "create-route-failure"),
			Eventf(corev1.EventTypeWarning, "CreationFailed", "Failed to create Route %q: %v",
				"create-route-failure", "inducing failure for create routes"),
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to create Route: inducing failure for create routes"),
		},
	}, {
		Name: "configuration creation failure",
		// Induce a failure during configuration creation
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "configurations"),
		},
		Objects: []runtime.Object{
			DefaultService("create-config-failure", "foo", WithRunLatestRollout, WithServiceGeneration(1)),
		},
		Key: "foo/create-config-failure",
		WantCreates: []runtime.Object{
			config("create-config-failure", "foo", WithRunLatestRollout),
			// We don't get to creating the Route.
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("create-config-failure", "foo", WithRunLatestRollout,
				// First reconcile initializes conditions.
				WithInitSvcConditions, WithServiceObservedGenFailure,
				WithServiceGeneration(1), WithServiceObservedGeneration),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "CreationFailed", "Failed to create Configuration %q: %v",
				"create-config-failure", "inducing failure for create configurations"),
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to create Configuration: inducing failure for create configurations"),
		},
	}, {
		Name: "update route failure",
		// Induce a failure updating the route
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "routes"),
		},
		Objects: []runtime.Object{
			DefaultService("update-route-failure", "foo", WithRunLatestRollout,
				WithInitSvcConditions),
			// Mutate the Route to have an unexpected body to trigger an update.
			route("update-route-failure", "foo", WithRunLatestRollout, MutateRoute),
			config("update-route-failure", "foo", WithRunLatestRollout),
		},
		Key: "foo/update-route-failure",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("update-route-failure", "foo", WithRunLatestRollout),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to reconcile Route: inducing failure for update routes"),
		},
	}, {
		Name: "update configuration failure",
		// Induce a failure updating the config
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "configurations"),
		},
		Objects: []runtime.Object{
			DefaultService("update-config-failure", "foo", WithRunLatestRollout,
				WithInitSvcConditions, WithServiceGeneration(1), WithServiceObservedGeneration),
			route("update-config-failure", "foo", WithRunLatestRollout),
			// Mutate the Config to have an unexpected body to trigger an update.
			config("update-config-failure", "foo", WithRunLatestRollout,
				// This is just an unexpected mutation of the config spec vs. the service spec.
				WithConfigContainerConcurrency(5)),
		},
		Key: "foo/update-config-failure",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: config("update-config-failure", "foo", WithRunLatestRollout),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to reconcile Configuration: inducing failure for update configurations"),
		},
	}, {
		Name: "failure updating service status",
		// Induce a failure updating the service status.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "services"),
		},
		Objects: []runtime.Object{
			DefaultService("run-latest", "foo", WithRunLatestRollout, WithServiceGeneration(1)),
		},
		Key: "foo/run-latest",
		WantCreates: []runtime.Object{
			config("run-latest", "foo", WithRunLatestRollout),
			route("run-latest", "foo", WithRunLatestRollout),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("run-latest", "foo", WithRunLatestRollout,
				// We attempt to update the Service to initialize its
				// conditions, which is where we induce the failure.
				WithInitSvcConditions, WithServiceObservedGenFailure,
				WithServiceGeneration(1), WithServiceObservedGeneration),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Configuration %q", "run-latest"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Route %q", "run-latest"),
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "Failed to update status for %q: %v",
				"run-latest", "inducing failure for update services"),
		},
	}, {
		Name: "route and config ready, propagate ready",
		// When both route and config are ready, the service should become ready.
		Objects: []runtime.Object{
			DefaultService("all-ready", "foo", WithRunLatestRollout, WithInitSvcConditions, WithServiceGeneration(1)),
			route("all-ready", "foo", WithRunLatestRollout, RouteReady,
				WithURL, WithAddress, WithInitRouteConditions,
				WithStatusTraffic(
					v1.TrafficTarget{
						RevisionName: "all-ready-00001",
						Percent:      ptr.Int64(100),
					}), MarkTrafficAssigned, MarkIngressReady),
			config("all-ready", "foo", WithRunLatestRollout,
				WithConfigGeneration(1), WithConfigObservedGen,
				// These turn a Configuration to Ready=true
				WithLatestCreated("all-ready-00001"), WithLatestReady("all-ready-00001")),
		},
		Key: "foo/all-ready",
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("all-ready", "foo", WithRunLatestRollout,
				WithReadyConfig("all-ready-00001"),
				// The delta induced by route object.
				WithReadyRoute, WithSvcStatusDomain, WithSvcStatusAddress,
				WithSvcStatusTraffic(v1.TrafficTarget{
					RevisionName: "all-ready-00001",
					Percent:      ptr.Int64(100),
				})),
		}},
	}, {
		Name: "configuration lagging",
		// When both route and config are ready, the service should become ready.
		Objects: []runtime.Object{
			DefaultService("all-ready", "foo", WithRunLatestRollout, WithInitSvcConditions,
				WithReadyConfig("all-ready-00001"), WithServiceGeneration(1)),
			route("all-ready", "foo", WithRunLatestRollout, RouteReady,
				WithURL, WithAddress, WithInitRouteConditions,
				WithStatusTraffic(v1.TrafficTarget{
					RevisionName: "all-ready-00001",
					Percent:      ptr.Int64(100),
				}), MarkTrafficAssigned, MarkIngressReady),
			config("all-ready", "foo", WithRunLatestRollout,
				WithConfigGeneration(1), WithConfigObservedGen, WithConfigGeneration(2),
				// These turn a Configuration to Ready=true
				WithLatestCreated("all-ready-00001"), WithLatestReady("all-ready-00001")),
		},
		Key: "foo/all-ready",
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("all-ready", "foo", WithRunLatestRollout,
				WithReadyConfig("all-ready-00001"),
				// The delta induced by route object.
				WithReadyRoute, WithSvcStatusDomain, WithSvcStatusAddress,
				MarkConfigurationNotReconciled,
				WithSvcStatusTraffic(v1.TrafficTarget{
					RevisionName: "all-ready-00001",
					Percent:      ptr.Int64(100),
				})),
		}},
	}, {
		Name: "route ready previous version and config ready, service not ready",
		// When both route and config are ready, but the route points to the previous revision
		// the service should not be ready.
		Objects: []runtime.Object{
			DefaultService("config-only-ready", "foo", WithRunLatestRollout, WithInitSvcConditions, WithServiceGeneration(1)),
			route("config-only-ready", "foo", WithRunLatestRollout, RouteReady,
				WithURL, WithAddress, WithInitRouteConditions,
				WithStatusTraffic(v1.TrafficTarget{
					RevisionName: "config-only-ready-00001",
					Percent:      ptr.Int64(100),
				}), MarkTrafficAssigned, MarkIngressReady),
			config("config-only-ready", "foo", WithRunLatestRollout,
				WithConfigGeneration(2 /*will generate revision -00002*/), WithConfigObservedGen,
				// These turn a Configuration to Ready=true
				WithLatestCreated("config-only-ready-00002"), WithLatestReady("config-only-ready-00002")),
		},
		Key: "foo/config-only-ready",
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("config-only-ready", "foo", WithRunLatestRollout,
				WithReadyConfig("config-only-ready-00002"),
				WithServiceStatusRouteNotReady, WithSvcStatusDomain, WithSvcStatusAddress,
				WithSvcStatusTraffic(v1.TrafficTarget{
					RevisionName: "config-only-ready-00001",
					Percent:      ptr.Int64(100),
				})),
		}},
	}, {
		Name: "config fails, new gen, propagate failure",
		// Gen 1: everything is fine;
		// Gen 2: config update fails;
		//    => service is still OK serving Gen 1.
		Objects: []runtime.Object{
			DefaultService("config-fails", "foo", WithRunLatestRollout, WithInitSvcConditions, WithServiceGeneration(1)),
			route("config-fails", "foo", WithRunLatestRollout, RouteReady,
				WithURL, WithAddress, WithInitRouteConditions,
				WithStatusTraffic(v1.TrafficTarget{
					RevisionName: "config-fails-00001",
					Percent:      ptr.Int64(100),
				}), MarkTrafficAssigned, MarkIngressReady),
			config("config-fails", "foo", WithRunLatestRollout, WithConfigGeneration(2),
				WithLatestReady("config-fails-00001"), WithLatestCreated("config-fails-00002"),
				MarkLatestCreatedFailed("blah"), WithConfigObservedGen),
		},
		Key: "foo/config-fails",
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("config-fails", "foo", WithRunLatestRollout, WithInitSvcConditions,
				WithReadyRoute, WithSvcStatusDomain, WithSvcStatusAddress,
				WithSvcStatusTraffic(v1.TrafficTarget{
					RevisionName: "config-fails-00001",
					Percent:      ptr.Int64(100),
				}),
				WithFailedConfig("config-fails-00002", "RevisionFailed", "blah"),
				WithServiceLatestReadyRevision("config-fails-00001")),
		}},
	}, {
		Name: "configuration failure is propagated",
		// When config fails, the service should fail.
		Objects: []runtime.Object{
			DefaultService("config-fails", "foo", WithRunLatestRollout, WithInitSvcConditions, WithServiceGeneration(1)),
			route("config-fails", "foo", WithRunLatestRollout, RouteReady),
			config("config-fails", "foo", WithRunLatestRollout, WithConfigGeneration(1), WithConfigObservedGen,
				WithLatestCreated("config-fails-00001"), MarkLatestCreatedFailed("blah")),
		},
		Key: "foo/config-fails",
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("config-fails", "foo", WithRunLatestRollout, WithInitSvcConditions,
				WithServiceStatusRouteNotReady, WithFailedConfig(
					"config-fails-00001", "RevisionFailed", "blah")),
		}},
	}, {
		Name: "route failure is propagated",
		// When route fails, the service should fail.
		Objects: []runtime.Object{
			DefaultService("route-fails", "foo", WithRunLatestRollout, WithInitSvcConditions, WithServiceGeneration(1)),
			route("route-fails", "foo", WithRunLatestRollout,
				RouteFailed("Propagate me, please", "")),
			config("route-fails", "foo", WithRunLatestRollout, WithConfigGeneration(1), WithConfigObservedGen,
				// These turn a Configuration to Ready=true
				WithLatestCreated("route-fails-00001"), WithLatestReady("route-fails-00001")),
		},
		Key: "foo/route-fails",
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("route-fails", "foo", WithRunLatestRollout, WithInitSvcConditions,
				// When the Configuration is Ready, and the Route has failed,
				// we expect the following changed to our status conditions.
				WithReadyConfig("route-fails-00001"),
				WithFailedRoute("Propagate me, please", "")),
		}},
	}, {
		Name:    "existing configuration without an owner causes a failure",
		WantErr: true,
		Objects: []runtime.Object{
			DefaultService("run-latest", "foo", WithRunLatestRollout, WithServiceGeneration(1)),
			config("run-latest", "foo", WithRunLatestRollout, WithConfigOwnersRemoved),
		},
		Key: "foo/run-latest",
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("run-latest", "foo", WithRunLatestRollout,
				// The first reconciliation will initialize the status conditions.
				WithInitSvcConditions, MarkConfigurationNotOwned,
				WithServiceGeneration(1), WithServiceObservedGeneration),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `service: "run-latest" does not own configuration: "run-latest"`),
		},
	}, {
		Name:    "existing route without an owner causes a failure",
		WantErr: true,
		Objects: []runtime.Object{
			DefaultService("run-latest", "foo", WithRunLatestRollout, WithServiceGeneration(1)),
			config("run-latest", "foo", WithRunLatestRollout),
			route("run-latest", "foo", WithRunLatestRollout, WithRouteOwnersRemoved),
		},
		Key: "foo/run-latest",
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("run-latest", "foo", WithRunLatestRollout,
				// The first reconciliation will initialize the status conditions.
				WithInitSvcConditions, MarkRouteNotOwned, WithServiceGeneration(1), WithServiceObservedGeneration),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `service: "run-latest" does not own route: "run-latest"`),
		},
	}, {
		Name: "correct not owned status if ownership issues are resolved",
		// If ready Route/Configuration that weren't owned have OwnerReferences attached,
		// then a Reconcile will result in the Service becoming happy.
		Objects: []runtime.Object{
			DefaultService("new-owner", "foo", WithRunLatestRollout, WithInitSvcConditions,
				// This service was unhappy with the prior owner situation.
				MarkConfigurationNotOwned, MarkRouteNotOwned),
			// The service owns these, which should result in a happy result.
			route("new-owner", "foo", WithRunLatestRollout, RouteReady,
				WithURL, WithAddress, WithInitRouteConditions,
				WithStatusTraffic(v1.TrafficTarget{
					RevisionName: "new-owner-00001",
					Percent:      ptr.Int64(100),
				}), MarkTrafficAssigned, MarkIngressReady),
			config("new-owner", "foo", WithRunLatestRollout, WithConfigGeneration(1), WithConfigObservedGen,
				// These turn a Configuration to Ready=true
				WithLatestCreated("new-owner-00001"), WithLatestReady("new-owner-00001")),
		},
		Key: "foo/new-owner",
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DefaultService("new-owner", "foo", WithRunLatestRollout,
				WithReadyConfig("new-owner-00001"),
				// The delta induced by route object.
				WithReadyRoute, WithSvcStatusDomain, WithSvcStatusAddress,
				WithSvcStatusTraffic(v1.TrafficTarget{
					RevisionName: "new-owner-00001",
					Percent:      ptr.Int64(100),
				})),
		}},
	}, {
		// Config should not be updated because no new changes besides new default
		Name: "service is has new defaults after upgrade; config should not be updated",
		Objects: []runtime.Object{
			DefaultService("release-no-change-config", "foo", WithInitSvcConditions, WithRunLatestRollout),
			config("release-no-change-config", "foo", WithRunLatestRollout,
				func(configuration *v1.Configuration) {
					// The ContainerConcurrency is not set here, but it is set on the default service.
					// The reconciler should ignore this difference because after setting on default on the
					// config will cause ContainerConcurrency here set to the default value (same as in service),
					// and therefore would be no diff.
					configuration.Spec.Template.Spec.ContainerConcurrency = nil
				},
			),
			route("release-no-change-config", "foo", WithRunLatestRollout),
		},
		Key: "foo/release-no-change-config",
	}, {
		// Route should not be updated because no new changes besides new default
		Name: "service is has new defaults after upgrade; route should not be updated",
		Objects: []runtime.Object{
			DefaultService("release-no-change-route", "foo", WithInitSvcConditions, WithRunLatestRollout,
				WithSvcStatusTraffic(v1.TrafficTarget{
					Percent:           ptr.Int64(100),
					ConfigurationName: "release-no-change-route",
					LatestRevision:    ptr.Bool(true),
				}),
				WithSvcStatusTraffic(v1.TrafficTarget{
					Percent:           ptr.Int64(100),
					ConfigurationName: "release-no-change-route",
					LatestRevision:    ptr.Bool(true),
				}),
			),
			config("release-no-change-route", "foo", WithRunLatestRollout),
			route("release-no-change-route", "foo", WithRunLatestRollout,
				func(ro *v1.Route) {
					ro.Spec.Traffic = []v1.TrafficTarget{{
						// The LatestRevision is not set here, but it is set on the service status traffic.
						// The reconciler should ignore this difference because after setting on default on the
						// route will cause LatestRevision here set to true, and therefore would be no diff.
						Percent:           ptr.Int64(100),
						ConfigurationName: "release-no-change-route",
					}}
					ro.Status.RouteStatusFields.Traffic = []v1.TrafficTarget{{
						Percent:           ptr.Int64(100),
						ConfigurationName: "release-no-change-route",
						LatestRevision:    ptr.Bool(true),
					}}
				},
			),
		},
		Key: "foo/release-no-change-route",
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		retryAttempted = false
		r := &Reconciler{
			client:              servingclient.Get(ctx),
			configurationLister: listers.GetConfigurationLister(),
			revisionLister:      listers.GetRevisionLister(),
			routeLister:         listers.GetRouteLister(),
		}

		return ksvcreconciler.NewReconciler(ctx, logging.FromContext(ctx), servingclient.Get(ctx),
			listers.GetServiceLister(), controller.GetEventRecorder(ctx), r)
	}))
}

func TestNew(t *testing.T) {
	ctx, _ := SetupFakeContext(t)

	configMapWatcher := configmap.NewStaticWatcher(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfgmap.FeaturesConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfgmap.DefaultsConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      autoscalercfg.ConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{},
	})

	c := NewController(ctx, configMapWatcher)

	if c == nil {
		t.Fatal("Expected NewController to return a non-nil value")
	}
}

func config(name, namespace string, so ServiceOption, co ...ConfigOption) *v1.Configuration {
	s := DefaultService(name, namespace, so)
	s.SetDefaults(context.Background())
	cfg, err := resources.MakeConfiguration(s)
	if err != nil {
		panic(fmt.Sprint("MakeConfiguration() = ", err))
	}
	for _, opt := range co {
		opt(cfg)
	}
	return cfg
}

func route(name, namespace string, so ServiceOption, ro ...RouteOption) *v1.Route {
	s := DefaultService(name, namespace, so)
	s.SetDefaults(context.Background())
	route, err := resources.MakeRoute(s)
	if err != nil {
		panic(fmt.Sprint("MakeRoute() = ", err))
	}
	for _, opt := range ro {
		opt(route)
	}
	return route
}

// TODO(mattmoor): Replace these when we refactor Route's table_test.go
func MutateRoute(rt *v1.Route) {
	rt.Spec = v1.RouteSpec{}
}

func RouteReady(cfg *v1.Route) {
	cfg.Status = v1.RouteStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   "Ready",
				Status: "True",
			}},
		},
	}
}

func RouteFailed(reason, message string) RouteOption {
	return func(cfg *v1.Route) {
		cfg.Status = v1.RouteStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:    "Ready",
					Status:  "False",
					Reason:  reason,
					Message: message,
				}},
			},
		}
	}
}

func rev(name, namespace string, so ServiceOption, co ...ConfigOption) *v1.Revision {
	cfg := config(name, namespace, so, co...)
	return configresources.MakeRevision(context.Background(), cfg, clock.RealClock{})
}
