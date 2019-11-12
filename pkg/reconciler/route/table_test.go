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

package route

import (
	"context"
	"fmt"
	"testing"
	"time"

	"knative.dev/serving/pkg/apis/networking"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	clientgotesting "k8s.io/client-go/testing"

	"knative.dev/pkg/apis"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/tracker"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/gc"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/reconciler"
	kaccessor "knative.dev/serving/pkg/reconciler/accessor"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/resources"
	"knative.dev/serving/pkg/reconciler/route/traffic"

	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1alpha1"
	. "knative.dev/serving/pkg/testing"
	. "knative.dev/serving/pkg/testing/v1alpha1"
)

const TestIngressClass = "ingress-class-foo"

var fakeCurTime = time.Unix(1e9, 0)

// This is heavily based on the way the OpenShift Ingress controller tests its reconciliation method.
func TestReconcile(t *testing.T) {
	table := TableTest{{
		Name: "bad workqueue key",
		// Make sure Reconcile handles bad keys.
		Key: "too/many/parts",
	}, {
		Name: "key not found",
		// Make sure Reconcile handles good keys that don't exist.
		Key: "foo/not-found",
	}, {
		Name: "configuration not yet ready",
		Objects: []runtime.Object{
			Route("default", "first-reconcile", WithConfigTarget("not-ready")),
			cfg("default", "not-ready", WithGeneration(1), WithLatestCreated("not-ready-00001")),
			rev("default", "not-ready", 1, WithInitRevConditions, WithRevName("not-ready-00001")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "first-reconcile", WithConfigTarget("not-ready"), WithURL,
				// The first reconciliation initializes the conditions and reflects
				// that the referenced configuration is not yet ready.
				WithInitRouteConditions, MarkConfigurationNotReady("not-ready")),
		}},
		Key: "default/first-reconcile",
	}, {
		Name: "configuration permanently failed",
		Objects: []runtime.Object{
			Route("default", "first-reconcile", WithConfigTarget("permanently-failed")),
			cfg("default", "permanently-failed",
				WithGeneration(1), WithLatestCreated("permanently-failed-00001"), MarkLatestCreatedFailed("blah")),
			rev("default", "permanently-failed", 1,
				WithRevName("permanently-failed-00001"),
				WithInitRevConditions, MarkContainerMissing),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "first-reconcile", WithConfigTarget("permanently-failed"), WithURL,
				WithInitRouteConditions, MarkConfigurationFailed("permanently-failed")),
		}},
		Key: "default/first-reconcile",
	}, {
		Name:    "failure updating route status",
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "routes"),
		},
		Objects: []runtime.Object{
			Route("default", "first-reconcile", WithConfigTarget("not-ready")),
			cfg("default", "not-ready", WithGeneration(1), WithLatestCreated("not-ready-00001")),
			rev("default", "not-ready", 1, WithInitRevConditions, WithRevName("not-ready-00001")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "first-reconcile", WithConfigTarget("not-ready"), WithURL,
				WithInitRouteConditions, MarkConfigurationNotReady("not-ready")),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "Failed to update status for Route %q: %v",
				"first-reconcile", "inducing failure for update routes"),
		},
		Key: "default/first-reconcile",
	}, {
		Name: "simple route becomes ready, ingress unknown",
		Objects: []runtime.Object{
			Route("default", "becomes-ready", WithConfigTarget("config"), WithRouteUID("12-34")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001")),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001"), WithServiceName("mcd")),
		},
		WantCreates: []runtime.Object{
			simpleIngress(
				Route("default", "becomes-ready", WithConfigTarget("config"), WithURL,
					WithRouteUID("12-34")),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "mcd",
							Active:      true,
						}},
					},
				},
			),
			simplePlaceholderK8sService(
				getContext(),
				Route("default", "becomes-ready", WithConfigTarget("config"), WithRouteUID("12-34")),
				"",
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "becomes-ready", WithConfigTarget("config"),
				WithRouteUID("12-34"),
				// Populated by reconciliation when all traffic has been assigned.
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressNotConfigured, WithStatusTraffic(v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						RevisionName:   "config-00001",
						Percent:        ptr.Int64(100),
						LatestRevision: ptr.Bool(true),
					},
				})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created placeholder service %q", "becomes-ready"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "becomes-ready"),
		},
		Key: "default/becomes-ready",
	}, {
		Name: "custom ingress route becomes ready, ingress unknown",
		Objects: []runtime.Object{
			Route("default", "becomes-ready",
				WithConfigTarget("config"), WithRouteUID("12-34"), WithIngressClass("custom-ingress-class")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001")),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001"), WithServiceName("bk")),
		},
		WantCreates: []runtime.Object{
			ingressWithClass(
				Route("default", "becomes-ready", WithConfigTarget("config"), WithURL, WithRouteUID("12-34")),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "bk",
							Active:      true,
						}},
					},
				},
				"custom-ingress-class",
				sets.NewString(),
			),
			simplePlaceholderK8sService(
				getContext(),
				Route("default", "becomes-ready",
					WithConfigTarget("config"), WithRouteUID("12-34"), WithIngressClass("custom-ingress-class")),
				"",
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "becomes-ready", WithConfigTarget("config"),
				WithRouteUID("12-34"), WithIngressClass("custom-ingress-class"),
				// Populated by reconciliation when all traffic has been assigned.
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressNotConfigured, WithStatusTraffic(v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						RevisionName:   "config-00001",
						Percent:        ptr.Int64(100),
						LatestRevision: ptr.Bool(true),
					},
				})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created placeholder service %q", "becomes-ready"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "becomes-ready"),
		},
		Key: "default/becomes-ready",
	}, {
		Name: "cluster local route becomes ready, ingress unknown",
		Objects: []runtime.Object{
			Route("default", "becomes-ready", WithConfigTarget("config"), WithLocalDomain,
				WithRouteLabel(map[string]string{"serving.knative.dev/visibility": "cluster-local"}),
				WithRouteUID("65-23")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001")),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001"), WithServiceName("tb")),
		},
		WantCreates: []runtime.Object{
			simpleIngressWithVisibility(
				Route("default", "becomes-ready", WithConfigTarget("config"),
					WithLocalDomain, WithRouteUID("65-23"),
					WithRouteLabel(map[string]string{"serving.knative.dev/visibility": "cluster-local"})),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "tb",
							Active:      true,
						}},
					},
				},
				sets.NewString("becomes-ready"),
			),
			simplePlaceholderK8sService(
				getContext(),
				Route("default", "becomes-ready", WithConfigTarget("config"), WithLocalDomain,
					WithRouteLabel(map[string]string{"serving.knative.dev/visibility": "cluster-local"}),
					WithRouteUID("65-23")),
				"",
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "becomes-ready", WithConfigTarget("config"),
				WithRouteUID("65-23"),
				// Populated by reconciliation when all traffic has been assigned.
				WithLocalDomain, WithAddress, WithInitRouteConditions,
				WithRouteLabel(map[string]string{"serving.knative.dev/visibility": "cluster-local"}),
				MarkTrafficAssigned, MarkIngressNotConfigured, WithStatusTraffic(v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						RevisionName:   "config-00001",
						Percent:        ptr.Int64(100),
						LatestRevision: ptr.Bool(true),
					},
				})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created placeholder service %q", "becomes-ready"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "becomes-ready"),
		},
		Key: "default/becomes-ready",
	}, {
		Name: "simple route becomes ready",
		Objects: []runtime.Object{
			Route("default", "becomes-ready", WithConfigTarget("config")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001")),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001")),
			simpleReadyIngress(
				Route("default", "becomes-ready", WithConfigTarget("config"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							Active: true,
						}},
					},
				},
			),
		},
		WantCreates: []runtime.Object{
			simplePlaceholderK8sService(getContext(), Route("default", "becomes-ready", WithConfigTarget("config")), ""),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			{Object: simpleK8sService(Route("default", "becomes-ready", WithConfigTarget("config")))},
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "becomes-ready", WithConfigTarget("config"),
				// Populated by reconciliation when the route becomes ready.
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "config-00001",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created placeholder service %q", "becomes-ready"),
		},
		Key: "default/becomes-ready",
	}, {
		Name: "failure creating k8s placeholder service",
		// We induce a failure creating the placeholder service
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "services"),
		},
		Objects: []runtime.Object{
			Route("default", "create-svc-failure", WithConfigTarget("config"), WithRouteFinalizer),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001")),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001")),
		},
		WantCreates: []runtime.Object{
			simplePlaceholderK8sService(getContext(), Route("default", "create-svc-failure", WithConfigTarget("config")), ""),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "create-svc-failure", WithConfigTarget("config"),
				WithRouteFinalizer,
				// Populated by reconciliation when we've failed to create
				// the K8s service.
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "config-00001",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "CreationFailed", "Failed to create placeholder service %q: %v",
				"create-svc-failure", "inducing failure for create services"),
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to create placeholder service: inducing failure for create services"),
		},
		Key: "default/create-svc-failure",
	}, {
		Name: "failure creating ingress",
		Objects: []runtime.Object{
			Route("default", "ingress-create-failure", WithConfigTarget("config"), WithRouteFinalizer),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001")),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001"), WithServiceName("astrid")),
		},
		// We induce a failure creating the Ingress.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "ingresses"),
		},
		WantCreates: []runtime.Object{
			//This is the Create we see for the ingress, but we induce a failure.
			simpleIngress(
				Route("default", "ingress-create-failure", WithConfigTarget("config"),
					WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "astrid",
							Active:      true,
						}},
					},
				},
			),
			func() *corev1.Service {
				result, _ := resources.MakeK8sPlaceholderService(getContext(),
					Route("default", "ingress-create-failure", WithConfigTarget("config"), WithRouteFinalizer),
					"")
				return result
			}(),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "ingress-create-failure", WithConfigTarget("config"),
				WithRouteFinalizer,
				// Populated by reconciliation when we fail to create
				// the cluster ingress.
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, WithStatusTraffic(v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						RevisionName:   "config-00001",
						Percent:        ptr.Int64(100),
						LatestRevision: ptr.Bool(true),
					},
				})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created placeholder service %q", "ingress-create-failure"),
			Eventf(corev1.EventTypeWarning, "CreationFailed", "Failed to create Ingress: inducing failure for create ingresses"),
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to create Ingress: inducing failure for create ingresses"),
		},
		Key: "default/ingress-create-failure",
	}, {
		Name: "steady state",
		Objects: []runtime.Object{
			Route("default", "steady-state", WithConfigTarget("config"),
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady,
				WithRouteFinalizer, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "config-00001",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001"),
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "steady-state"),
			),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001")),
			simpleReadyIngress(
				Route("default", "steady-state", WithConfigTarget("config"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(Route("default", "steady-state", WithConfigTarget("config"))),
		},
		Key: "default/steady-state",
	}, {
		Name:    "unhappy about ownership of placeholder service",
		WantErr: true,
		Objects: []runtime.Object{
			Route("default", "unhappy-owner", WithConfigTarget("config"),
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithRouteFinalizer, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName: "config-00001",
							Percent:      ptr.Int64(100),
						},
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001"),
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "unhappy-owner"),
			),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001")),
			simpleK8sService(Route("default", "unhappy-owner", WithConfigTarget("config")),
				WithK8sSvcOwnersRemoved),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "unhappy-owner", WithConfigTarget("config"),
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithRouteFinalizer, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "config-00001",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					}),
				// The owner is not us, so we are unhappy.
				MarkServiceNotOwned),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `route: "unhappy-owner" does not own Service: "unhappy-owner"`),
		},
		Key: "default/unhappy-owner",
	}, {
		// This tests that when the Route is labelled differently, it is configured with a
		// different domain from config-domain.yaml.  This is otherwise a copy of the steady
		// state test above.
		Name: "different labels, different domain - steady state",
		Objects: []runtime.Object{
			Route("default", "different-domain", WithConfigTarget("config"),
				WithAnotherDomain, WithAddress,
				WithInitRouteConditions, MarkTrafficAssigned, MarkIngressReady,
				WithRouteFinalizer, WithStatusTraffic(v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						RevisionName:   "config-00001",
						Percent:        ptr.Int64(100),
						LatestRevision: ptr.Bool(true),
					},
				}), WithRouteLabel(map[string]string{"app": "prod"})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001"),
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "different-domain"),
			),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001"), WithServiceName("my-service")),
			simpleReadyIngress(
				Route("default", "different-domain", WithConfigTarget("config"),
					WithAnotherDomain),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							Active:      true,
							ServiceName: "my-service",
						}},
					},
				},
			),
			simpleK8sService(Route("default", "different-domain", WithConfigTarget("config"))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			{
				Object: simpleReadyIngress(
					Route("default", "different-domain", WithConfigTarget("config"),
						WithAnotherDomain),
					&traffic.Config{
						Targets: map[string]traffic.RevisionTargets{
							traffic.DefaultTarget: {{
								TrafficTarget: v1.TrafficTarget{
									// Use the Revision name from the config.
									RevisionName: "config-00001",
									Percent:      ptr.Int64(100),
								},
								Active:      true,
								ServiceName: "my-service",
							}},
						},
					},
					WithHosts(
						0,
						"different-domain.default.svc.cluster.local",
						"different-domain.default.another-example.com",
					),
				),
			},
		},
		Key: "default/different-domain",
	}, {
		Name: "new latest created revision",
		Objects: []runtime.Object{
			Route("default", "new-latest-created", WithConfigTarget("config"),
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithRouteFinalizer, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "config-00001",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					})),
			cfg("default", "config",
				WithGeneration(2), WithLatestReady("config-00001"), WithLatestCreated("config-00002"),
				WithConfigLabel("serving.knative.dev/route", "new-latest-created"),
			),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001"), WithServiceName("daisy")),
			// This is the name of the new revision we're referencing above.
			rev("default", "config", 2, WithInitRevConditions, WithRevName("config-00002")),
			simpleReadyIngress(
				Route("default", "new-latest-created", WithConfigTarget("config"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "daisy",
							Active:      true,
						}},
					},
				},
			),
			simpleK8sService(Route("default", "new-latest-created", WithConfigTarget("config"))),
		},
		// A new LatestCreatedRevisionName on the Configuration alone should result in no changes to the Route.
		Key: "default/new-latest-created",
	}, {
		Name: "new latest ready revision",
		Objects: []runtime.Object{
			Route("default", "new-latest-ready", WithConfigTarget("config"),
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithRouteFinalizer, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName: "config-00001",
							Percent:      ptr.Int64(100),
						},
					})),
			cfg("default", "config",
				WithGeneration(2), WithLatestCreated("config-00002"), WithLatestReady("config-00002"),
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "new-latest-ready"),
			),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001"), WithServiceName("magnolia")),
			// This is the name of the new revision we're referencing above.
			rev("default", "config", 2, MarkRevisionReady, WithRevName("config-00002"), WithServiceName("belltown")),
			simpleReadyIngress(
				Route("default", "new-latest-ready", WithConfigTarget("config"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "magnolia",
							Active:      true,
						}},
					},
				},
			),
			simpleK8sService(Route("default", "new-latest-ready", WithConfigTarget("config"))),
		},
		// A new LatestReadyRevisionName on the Configuration should result in the new Revision being rolled out.
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleReadyIngress(
				Route("default", "new-latest-ready", WithConfigTarget("config"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// This is the new config we're making become ready.
								RevisionName: "config-00002",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "belltown",
							Active:      true,
						}},
					},
				},
			),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "new-latest-ready", WithConfigTarget("config"),
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithRouteFinalizer, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "config-00002",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					})),
		}},
		Key: "default/new-latest-ready",
	}, {
		Name: "public becomes cluster local",
		Objects: []runtime.Object{
			Route("default", "becomes-local", WithConfigTarget("config"),
				WithRouteLabel(map[string]string{"serving.knative.dev/visibility": "cluster-local"}),
				WithRouteUID("65-23")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001")),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001"), WithServiceName("tb")),
			simpleIngress(
				Route("default", "becomes-local", WithConfigTarget("config"), WithRouteUID("65-23"), WithRouteLabel(map[string]string{"serving.knative.dev/visibility": "cluster-local"})),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "tb",
							Active:      true,
						}},
					},
				},
			),
			simpleK8sService(Route("default", "becomes-local", WithConfigTarget("config"),
				WithRouteLabel(map[string]string{"serving.knative.dev/visibility": "cluster-local"}),
				WithRouteUID("65-23"))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleIngressWithVisibility(
				Route("default", "becomes-local", WithConfigTarget("config"),
					WithRouteUID("65-23"),
					WithRouteLabel(map[string]string{"serving.knative.dev/visibility": "cluster-local"})),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "tb",
							Active:      true,
						}},
					},
				},
				sets.NewString("becomes-local"),
			),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "becomes-local", WithConfigTarget("config"),
				WithRouteUID("65-23"),
				MarkTrafficAssigned, MarkIngressNotConfigured,
				WithLocalDomain, WithAddress, WithInitRouteConditions,
				WithRouteLabel(map[string]string{"serving.knative.dev/visibility": "cluster-local"}),
				WithStatusTraffic(v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						RevisionName:   "config-00001",
						Percent:        ptr.Int64(100),
						LatestRevision: ptr.Bool(true),
					},
				})),
		}},
		Key: "default/becomes-local",
	}, {
		Name: "cluster local becomes public",
		Objects: []runtime.Object{
			Route("default", "becomes-public", WithConfigTarget("config"),
				WithRouteUID("65-23")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001")),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001"), WithServiceName("tb")),
			simpleIngressWithVisibility(
				Route("default", "becomes-public", WithConfigTarget("config"), WithRouteUID("65-23"),
					WithRouteLabel(map[string]string{"serving.knative.dev/visibility": "cluster-local"})),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "tb",
							Active:      true,
						}},
					},
				},
				sets.NewString("becomes-public"),
			),
			simpleK8sService(Route("default", "becomes-public", WithConfigTarget("config"),
				WithRouteUID("65-23"))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleIngress(
				Route("default", "becomes-public", WithConfigTarget("config"),
					WithRouteUID("65-23"), WithRouteLabel(map[string]string{"serving.knative.dev/visibility": "cluster-local"})),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "tb",
							Active:      true,
						}},
					},
				},
			),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "becomes-public", WithConfigTarget("config"),
				WithRouteUID("65-23"),
				MarkTrafficAssigned, MarkIngressNotConfigured,
				WithAddress, WithInitRouteConditions, WithURL,
				WithStatusTraffic(v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						RevisionName:   "config-00001",
						Percent:        ptr.Int64(100),
						LatestRevision: ptr.Bool(true),
					},
				})),
		}},
		Key: "default/becomes-public",
	}, {
		Name: "failure updating cluster ingress",
		// Starting from the new latest ready, induce a failure updating the cluster ingress.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "ingresses"),
		},
		Objects: []runtime.Object{
			Route("default", "update-ci-failure", WithConfigTarget("config"),
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithRouteFinalizer, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName: "config-00001",
							Percent:      ptr.Int64(100),
						},
					})),
			cfg("default", "config",
				WithGeneration(2), WithLatestCreated("config-00002"), WithLatestReady("config-00002"),
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "update-ci-failure"),
			),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001"), WithServiceName("fremont")),
			// This is the name of the new revision we're referencing above.
			rev("default", "config", 2, MarkRevisionReady, WithRevName("config-00002"), WithServiceName("wallingford")),
			simpleReadyIngress(
				Route("default", "update-ci-failure", WithConfigTarget("config"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							// Use the Revision name from the config.
							TrafficTarget: v1.TrafficTarget{
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "fremont",
							Active:      true,
						}},
					},
				},
			),
			simpleK8sService(Route("default", "update-ci-failure", WithConfigTarget("config"))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleReadyIngress(
				Route("default", "update-ci-failure", WithConfigTarget("config"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// This is the new config we're making become ready.
								RevisionName: "config-00002",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "wallingford",
							Active:      true,
						}},
					},
				},
			),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "update-ci-failure", WithConfigTarget("config"),
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithRouteFinalizer, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "config-00002",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to update Ingress: inducing failure for update ingresses"),
		},
		Key: "default/update-ci-failure",
	}, {
		Name: "reconcile service mutation",
		Objects: []runtime.Object{
			Route("default", "svc-mutation", WithConfigTarget("config"),
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithRouteFinalizer, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "config-00001",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001"),
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "svc-mutation"),
			),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001")),
			simpleReadyIngress(
				Route("default", "svc-mutation", WithConfigTarget("config"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(Route("default", "svc-mutation",
				WithConfigTarget("config")), MutateK8sService),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleK8sService(Route("default", "svc-mutation", WithConfigTarget("config"))),
		}},
		Key: "default/svc-mutation",
	}, {
		Name: "failure updating k8s service",
		// We start from the service mutation test, but induce a failure updating the service resource.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "services"),
		},
		Objects: []runtime.Object{
			Route("default", "svc-mutation", WithConfigTarget("config"), WithRouteFinalizer,
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "config-00001",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001"),
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "svc-mutation"),
			),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001")),
			simpleReadyIngress(
				Route("default", "svc-mutation", WithConfigTarget("config"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(Route("default", "svc-mutation",
				WithConfigTarget("config")), MutateK8sService),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleK8sService(Route("default", "svc-mutation", WithConfigTarget("config"))),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "inducing failure for update services"),
		},
		Key: "default/svc-mutation",
	}, {
		// In #1789 we switched this to an ExternalName Service. Services created in
		// 0.1 will still have ClusterIP set, which is Forbidden for ExternalName
		// Services. Ensure that we drop the ClusterIP if it is set in the spec.
		Name: "drop cluster ip",
		Objects: []runtime.Object{
			Route("default", "cluster-ip", WithConfigTarget("config"), WithRouteFinalizer,
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "config-00001",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001"),
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "cluster-ip"),
			),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001")),
			simpleReadyIngress(
				Route("default", "cluster-ip", WithConfigTarget("config"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(Route("default", "cluster-ip",
				WithConfigTarget("config")), WithClusterIP("127.0.0.1")),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleK8sService(Route("default", "cluster-ip", WithConfigTarget("config"))),
		}},
		Key: "default/cluster-ip",
	}, {
		// Make sure we fix the external name if something messes with it.
		Name: "fix external name",
		Objects: []runtime.Object{
			Route("default", "external-name", WithConfigTarget("config"), WithRouteFinalizer,
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "config-00001",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001"),
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "external-name"),
			),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001")),
			simpleReadyIngress(
				Route("default", "external-name", WithConfigTarget("config"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(Route("default", "external-name",
				WithConfigTarget("config")), WithExternalName("this-is-the-wrong-name")),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleK8sService(Route("default", "external-name", WithConfigTarget("config"))),
		}},
		Key: "default/external-name",
	}, {
		Name: "reconcile cluster ingress mutation",
		Objects: []runtime.Object{
			Route("default", "ingress-mutation", WithConfigTarget("config"), WithRouteFinalizer,
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "config-00001",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001"),
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "ingress-mutation"),
			),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001"), WithServiceName("windemere")),
			mutateIngress(simpleReadyIngress(
				Route("default", "ingress-mutation", WithConfigTarget("config"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "magnusson-park",
							Active:      true,
						}},
					},
				},
			)),
			simpleK8sService(Route("default", "ingress-mutation", WithConfigTarget("config"))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleReadyIngress(
				Route("default", "ingress-mutation", WithConfigTarget("config"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "windemere",
							Active:      true,
						}},
					},
				},
			),
		}},
		Key: "default/ingress-mutation",
	}, {
		Name: "switch to a different config",
		Objects: []runtime.Object{
			// The status reflects "oldconfig", but the spec "newconfig".
			Route("default", "change-configs", WithConfigTarget("newconfig"), WithRouteFinalizer,
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName: "oldconfig-00001",
							Percent:      ptr.Int64(100),
						},
					})),
			// Both configs exist, but only "oldconfig" is labelled.
			cfg("default", "oldconfig",
				WithGeneration(1), WithLatestCreated("oldconfig-00001"), WithLatestReady("oldconfig-00001"),
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "change-configs"),
			),
			cfg("default", "newconfig",
				WithGeneration(1), WithLatestCreated("newconfig-00001"), WithLatestReady("newconfig-00001")),
			rev("default", "oldconfig", 1, MarkRevisionReady, WithRevName("oldconfig-00001"), WithServiceName("greenwood")),
			rev("default", "newconfig", 1, MarkRevisionReady, WithRevName("newconfig-00001"), WithServiceName("broadview")),
			simpleReadyIngress(
				Route("default", "change-configs", WithConfigTarget("oldconfig"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "oldconfig-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "greenwood",
							Active:      true,
						}},
					},
				},
			),
			simpleK8sService(Route("default", "change-configs", WithConfigTarget("oldconfig"))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// Updated to point to "newconfig" things.
			Object: simpleReadyIngress(
				Route("default", "change-configs", WithConfigTarget("newconfig"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "newconfig-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "broadview",
							Active:      true,
						}},
					},
				},
			),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			// Status updated to "newconfig"
			Object: Route("default", "change-configs", WithConfigTarget("newconfig"),
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithRouteFinalizer, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "newconfig-00001",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					})),
		}},
		Key: "default/change-configs",
	}, {
		Name: "configuration missing",
		Objects: []runtime.Object{
			Route("default", "config-missing", WithConfigTarget("not-found")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "config-missing", WithConfigTarget("not-found"), WithURL,
				WithInitRouteConditions, MarkMissingTrafficTarget("Configuration", "not-found")),
		}},
		PostConditions: []func(*testing.T, *TableRow){
			AssertTrackingConfig("default", "not-found"),
		},
		Key: "default/config-missing",
	}, {
		Name: "revision missing (direct)",
		Objects: []runtime.Object{
			Route("default", "missing-revision-direct", WithRevTarget("not-found")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "missing-revision-direct", WithRevTarget("not-found"), WithURL,
				WithInitRouteConditions, MarkMissingTrafficTarget("Revision", "not-found")),
		}},
		PostConditions: []func(*testing.T, *TableRow){
			AssertTrackingRevision("default", "not-found"),
		},
		Key: "default/missing-revision-direct",
	}, {
		Name: "revision missing (indirect)",
		Objects: []runtime.Object{
			Route("default", "missing-revision-indirect", WithConfigTarget("config")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "missing-revision-indirect", WithConfigTarget("config"), WithURL,
				WithInitRouteConditions, MarkMissingTrafficTarget("Revision", "config-00001")),
		}},
		Key: "default/missing-revision-indirect",
	}, {
		Name: "pinned route becomes ready",
		Objects: []runtime.Object{
			Route("default", "pinned-becomes-ready",
				// Use the Revision name from the config
				WithRevTarget("config-00001"), WithRouteFinalizer,
			),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001")),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001")),
			simpleK8sService(Route("default", "pinned-becomes-ready", WithConfigTarget("config"))),
			simpleReadyIngress(
				Route("default", "pinned-becomes-ready", WithConfigTarget("config"),
					WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							Active: true,
						}},
					},
				},
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "pinned-becomes-ready",
				// Use the Revision name from the config
				WithRevTarget("config-00001"), WithRouteFinalizer,
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "config-00001",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(false),
						},
					})),
		}},
		Key: "default/pinned-becomes-ready",
	}, {
		Name: "traffic split becomes ready",
		Objects: []runtime.Object{
			Route("default", "named-traffic-split", WithSpecTraffic(
				v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "blue",
						Percent:           ptr.Int64(50),
					},
				}, v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "green",
						Percent:           ptr.Int64(50),
					},
				}), WithRouteUID("34-78"), WithRouteFinalizer),
			cfg("default", "blue",
				WithGeneration(1), WithLatestCreated("blue-00001"), WithLatestReady("blue-00001")),
			cfg("default", "green",
				WithGeneration(1), WithLatestCreated("green-00001"), WithLatestReady("green-00001")),
			rev("default", "blue", 1, MarkRevisionReady, WithRevName("blue-00001"), WithServiceName("blue-ridge")),
			rev("default", "green", 1, MarkRevisionReady, WithRevName("green-00001"), WithServiceName("green-lake")),
		},
		WantCreates: []runtime.Object{
			simpleIngress(
				Route("default", "named-traffic-split", WithURL, WithSpecTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							ConfigurationName: "blue",
							Percent:           ptr.Int64(50),
						},
					}, v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							ConfigurationName: "green",
							Percent:           ptr.Int64(50),
						},
					}), WithRouteUID("34-78")),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "blue-00001",
								Percent:      ptr.Int64(50),
							},
							ServiceName: "blue-ridge",
							Active:      true,
						}, {
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "green-00001",
								Percent:      ptr.Int64(50),
							},
							ServiceName: "green-lake",
							Active:      true,
						}},
					},
				},
			),
			simplePlaceholderK8sService(
				getContext(),
				Route("default", "named-traffic-split", WithSpecTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							ConfigurationName: "blue",
							Percent:           ptr.Int64(50),
						},
					}, v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							ConfigurationName: "green",
							Percent:           ptr.Int64(50),
						},
					}), WithRouteUID("34-78"), WithRouteFinalizer),
				"",
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "named-traffic-split", WithRouteFinalizer,
				WithSpecTraffic(v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "blue",
						Percent:           ptr.Int64(50),
					},
				}, v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "green",
						Percent:           ptr.Int64(50),
					},
				}), WithRouteUID("34-78"),
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressNotConfigured, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "blue-00001",
							Percent:        ptr.Int64(50),
							LatestRevision: ptr.Bool(true),
						},
					}, v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "green-00001",
							Percent:        ptr.Int64(50),
							LatestRevision: ptr.Bool(true),
						},
					})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created placeholder service %q", "named-traffic-split"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "named-traffic-split"),
		},
		Key: "default/named-traffic-split",
	}, {
		Name: "same revision targets",
		Objects: []runtime.Object{
			Route("default", "same-revision-targets", WithSpecTraffic(
				v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						Tag:               "gray",
						ConfigurationName: "gray",
						Percent:           ptr.Int64(50),
					},
				}, v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						Tag:          "also-gray",
						RevisionName: "gray-00001",
						Percent:      ptr.Int64(50),
					},
				}), WithRouteUID("1-2"), WithRouteFinalizer),
			cfg("default", "gray",
				WithGeneration(1), WithLatestCreated("gray-00001"), WithLatestReady("gray-00001")),
			rev("default", "gray", 1, MarkRevisionReady, WithRevName("gray-00001"), WithServiceName("shades")),
		},
		WantCreates: []runtime.Object{
			simpleIngress(
				Route("default", "same-revision-targets", WithURL, WithSpecTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							Tag:               "gray",
							ConfigurationName: "gray",
							Percent:           ptr.Int64(50),
						},
					}, v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							Tag:          "also-gray",
							RevisionName: "gray-00001",
							Percent:      ptr.Int64(50),
						},
					}), WithRouteUID("1-2")),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "gray-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "shades",
							Active:      true,
						}},
						"gray": {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "gray-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "shades",
							Active:      true,
						}},
						"also-gray": {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "gray-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "shades",
							Active:      true,
						}},
					},
				},
			),
			simplePlaceholderK8sService(
				getContext(),
				Route("default", "same-revision-targets", WithSpecTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							Tag:               "gray",
							ConfigurationName: "gray",
							Percent:           ptr.Int64(50),
						},
					}, v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							Tag:          "also-gray",
							RevisionName: "gray-00001",
							Percent:      ptr.Int64(50),
						},
					}), WithRouteUID("1-2"), WithRouteFinalizer),
				"",
			),
			simplePlaceholderK8sService(
				getContext(),
				Route("default", "same-revision-targets", WithSpecTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							Tag:               "gray",
							ConfigurationName: "gray",
							Percent:           ptr.Int64(50),
						},
					}, v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							Tag:          "also-gray",
							RevisionName: "gray-00001",
							Percent:      ptr.Int64(50),
						},
					}), WithRouteUID("1-2"), WithRouteFinalizer),
				"also-gray",
			),
			simplePlaceholderK8sService(
				getContext(),
				Route("default", "same-revision-targets", WithSpecTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							Tag:               "gray",
							ConfigurationName: "gray",
							Percent:           ptr.Int64(50),
						},
					}, v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							Tag:          "also-gray",
							RevisionName: "gray-00001",
							Percent:      ptr.Int64(50),
						},
					}), WithRouteUID("1-2"), WithRouteFinalizer),
				"gray",
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "same-revision-targets",
				WithSpecTraffic(v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						Tag:               "gray",
						ConfigurationName: "gray",
						Percent:           ptr.Int64(50),
					},
				}, v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						Tag:          "also-gray",
						RevisionName: "gray-00001",
						Percent:      ptr.Int64(50),
					},
				}), WithRouteUID("1-2"), WithRouteFinalizer,
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressNotConfigured, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							Tag:            "gray",
							RevisionName:   "gray-00001",
							Percent:        ptr.Int64(50),
							LatestRevision: ptr.Bool(true),
							URL: &apis.URL{
								Scheme: "http",
								Host:   "gray-same-revision-targets.default.example.com",
							},
						},
					}, v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							Tag:            "also-gray",
							RevisionName:   "gray-00001",
							Percent:        ptr.Int64(50),
							LatestRevision: ptr.Bool(false),
							URL: &apis.URL{
								Scheme: "http",
								Host:   "also-gray-same-revision-targets.default.example.com",
							},
						},
					})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created placeholder service %q", "same-revision-targets"),
			Eventf(corev1.EventTypeNormal, "Created", "Created placeholder service %q", "also-gray-same-revision-targets"),
			Eventf(corev1.EventTypeNormal, "Created", "Created placeholder service %q", "gray-same-revision-targets"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "same-revision-targets"),
		},
		Key: "default/same-revision-targets",
	}, {
		Name: "change route configuration",
		// Start from a steady state referencing "blue", and modify the route spec to point to "green" instead.
		Objects: []runtime.Object{
			Route("default", "switch-configs", WithConfigTarget("green"),
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							Tag:          "blue",
							RevisionName: "blue-00001",
							Percent:      ptr.Int64(100),
						},
					}), WithRouteFinalizer),
			cfg("default", "blue",
				WithGeneration(1), WithLatestCreated("blue-00001"), WithLatestReady("blue-00001"),
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "switch-configs"),
			),
			cfg("default", "green",
				WithGeneration(1), WithLatestCreated("green-00001"), WithLatestReady("green-00001")),
			rev("default", "blue", 1, MarkRevisionReady, WithRevName("blue-00001"), WithServiceName("alki-beach")),
			rev("default", "green", 1, MarkRevisionReady, WithRevName("green-00001"), WithServiceName("rainier-beach")),
			simpleReadyIngress(
				Route("default", "switch-configs", WithConfigTarget("blue"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "blue-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "alki-beach",
							Active:      true,
						}},
					},
				},
			),
			simpleK8sService(Route("default", "switch-configs", WithConfigTarget("blue"))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleReadyIngress(
				Route("default", "switch-configs", WithConfigTarget("green"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "green-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "rainier-beach",
							Active:      true,
						}},
					},
				},
			),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "switch-configs", WithConfigTarget("green"),
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "green-00001",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					}), WithRouteFinalizer),
		}},
		Key: "default/switch-configs",
	}, {
		Name: "update single target to traffic split with unready revision",
		// Start from a steady state referencing "blue", and modify the route spec to point to both
		// "blue" and "green" instead, while "green" is not ready.
		Objects: []runtime.Object{
			Route("default", "split", WithURL, WithAddress,
				WithInitRouteConditions, MarkTrafficAssigned, MarkIngressReady,
				WithSpecTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							ConfigurationName: "blue",
							Percent:           ptr.Int64(50),
						},
					}, v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							ConfigurationName: "green",
							Percent:           ptr.Int64(50),
						},
					}),
				WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							ConfigurationName: "blue",
							Percent:           ptr.Int64(100),
							LatestRevision:    ptr.Bool(true),
						},
					},
				)),
			cfg("default", "blue", WithGeneration(1),
				WithLatestCreated("blue-00001"), WithLatestReady("blue-00001")),
			cfg("default", "green", WithGeneration(1),
				WithLatestCreated("green-00001")),
			rev("default", "blue", 1, MarkRevisionReady, WithRevName("blue-00001")),
			rev("default", "green", 1, WithRevName("green-00001")),
			simpleK8sService(Route("default", "split", WithConfigTarget("blue"))),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "split", WithURL, WithAddress,
				WithInitRouteConditions, MarkTrafficAssigned, MarkIngressReady,
				MarkConfigurationNotReady("green"),
				WithSpecTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							ConfigurationName: "blue",
							Percent:           ptr.Int64(50),
						},
					}, v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							ConfigurationName: "green",
							Percent:           ptr.Int64(50),
						},
					}),
				WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							ConfigurationName: "blue",
							Percent:           ptr.Int64(100),
							LatestRevision:    ptr.Bool(true),
						},
					})),
		}},
		Key: "default/split",
	}, {
		Name: "Update stale lastPinned",
		Objects: []runtime.Object{
			Route("default", "stale-lastpinned", WithConfigTarget("config"),
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithRouteFinalizer, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "config-00001",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001"),
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "stale-lastpinned"),
			),
			rev("default", "config", 1, MarkRevisionReady,
				WithRevName("config-00001"),
				WithLastPinned(fakeCurTime.Add(-10*time.Minute))),
			simpleReadyIngress(
				Route("default", "stale-lastpinned", WithConfigTarget("config"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(Route("default", "stale-lastpinned", WithConfigTarget("config"))),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchLastPinned("default", "config-00001"),
		},
		Key: "default/stale-lastpinned",
	}, {
		Name: "check that we can find the cluster ingress with old naming",
		Objects: []runtime.Object{
			Route("default", "old-naming", WithConfigTarget("config"), WithRouteFinalizer,
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "config-00001",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					})),
			cfg("default", "config",
				WithGeneration(1),
				WithLatestCreated("config-00001"), WithLatestReady("config-00001"),
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "old-naming"),
			),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001")),
			simpleReadyIngress(
				Route("default", "old-naming", WithConfigTarget("config"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(Route("default", "old-naming", WithConfigTarget("config"))),
		},
		Key: "default/old-naming",
	}, {
		Name: "deletes service when route no longer references service",
		Objects: []runtime.Object{
			Route("default", "my-route", WithConfigTarget("config"),
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady,
				WithRouteFinalizer, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "config-00001",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001"),
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "steady-state"),
			),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001")),
			simpleReadyIngress(
				Route("default", "my-route", WithConfigTarget("config"), WithURL),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(Route("default", "my-route", WithConfigTarget("config"))),
			simpleK8sService(Route("default", "my-route"), OverrideServiceName("old-service-name")),
		},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: "default",
				Verb:      "delete",
				Resource: schema.GroupVersionResource{
					Group:    "core",
					Version:  "v1",
					Resource: "services",
				},
			},
			Name: "old-service-name",
		}},
		Key: "default/my-route",
	}, {
		Name:    "deletes service fails",
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("delete", "services"),
		},
		Objects: []runtime.Object{
			Route("default", "my-route", WithConfigTarget("config"),
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady,
				WithRouteFinalizer, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						TrafficTarget: v1.TrafficTarget{
							RevisionName:   "config-00001",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001"),
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "steady-state"),
			),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001")),
			simpleK8sService(Route("default", "my-route", WithConfigTarget("config"))),
			simpleK8sService(Route("default", "my-route"), OverrideServiceName("old-service-name")),
		},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: "default",
				Verb:      "delete",
				Resource: schema.GroupVersionResource{
					Group:    "core",
					Version:  "v1",
					Resource: "services",
				},
			},
			Name: "old-service-name",
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to delete Service: inducing failure for delete services"),
		},
		Key: "default/my-route",
	}}

	// TODO(mattmoor): Revision inactive (direct reference)
	// TODO(mattmoor): Revision inactive (indirect reference)
	// TODO(mattmoor): Multiple inactive Revisions

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		return &Reconciler{
			Base:                reconciler.NewBase(ctx, controllerAgentName, cmw),
			routeLister:         listers.GetRouteLister(),
			configurationLister: listers.GetConfigurationLister(),
			revisionLister:      listers.GetRevisionLister(),
			serviceLister:       listers.GetK8sServiceLister(),
			ingressLister:       listers.GetIngressLister(),
			tracker:             ctx.Value(TrackerKey).(tracker.Interface),
			configStore: &testConfigStore{
				config: ReconcilerTestConfig(false),
			},
			clock: FakeClock{Time: fakeCurTime},
		}
	}))
}

func TestReconcile_EnableAutoTLS(t *testing.T) {
	table := TableTest{{
		Name: "check that existing wildcard cert is used when creating a Route",
		Objects: []runtime.Object{
			wildcardCert("default", "example.com"),
			Route("default", "becomes-ready", WithConfigTarget("config"), WithRouteUID("12-34")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001")),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001"), WithServiceName("mcd")),
		},
		WantCreates: []runtime.Object{
			ingressWithTLS(
				Route("default", "becomes-ready", WithConfigTarget("config"), WithHTTPSDomain,
					WithRouteUID("12-34")),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "mcd",
							Active:      true,
						}},
					},
				},
				[]netv1alpha1.IngressTLS{
					{
						Hosts:           []string{"becomes-ready.default.example.com"},
						SecretName:      "default",
						SecretNamespace: "default",
					},
				},
			),
			simpleK8sService(
				Route("default", "becomes-ready", WithConfigTarget("config"), WithRouteUID("12-34")),
				WithExternalName("becomes-ready.default.example.com"),
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "becomes-ready", WithConfigTarget("config"),
				WithRouteUID("12-34"),
				// Populated by reconciliation when all traffic has been assigned.
				WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressNotConfigured, WithStatusTraffic(v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						RevisionName:   "config-00001",
						Percent:        ptr.Int64(100),
						LatestRevision: ptr.Bool(true),
					},
				}), WithReadyCertificateName("default.example.com"), WithHTTPSDomain),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created placeholder service %q", "becomes-ready"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "becomes-ready"),
		},
		Key: "default/becomes-ready",
	}, {
		Name: "check that Certificate and IngressTLS are correctly configured when creating a Route",
		Objects: []runtime.Object{
			Route("default", "becomes-ready", WithConfigTarget("config"), WithRouteUID("12-34")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001")),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001"), WithServiceName("mcd")),
		},
		WantCreates: []runtime.Object{
			resources.MakeCertificates(Route("default", "becomes-ready", WithConfigTarget("config"), WithURL, WithRouteUID("12-34")),
				map[string]string{"becomes-ready.default.example.com": ""}, network.CertManagerCertificateClassName)[0],
			ingressWithTLS(
				Route("default", "becomes-ready", WithConfigTarget("config"), WithURL,
					WithRouteUID("12-34")),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "mcd",
							Active:      true,
						}},
					},
				},
				[]netv1alpha1.IngressTLS{{
					Hosts:           []string{"becomes-ready.default.example.com"},
					SecretName:      "route-12-34",
					SecretNamespace: "default",
				}},
			),
			simpleK8sService(
				Route("default", "becomes-ready", WithConfigTarget("config"), WithRouteUID("12-34")),
				WithExternalName("becomes-ready.default.example.com"),
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "becomes-ready", WithConfigTarget("config"),
				WithRouteUID("12-34"),
				// Populated by reconciliation when all traffic has been assigned.
				WithURL, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressNotConfigured, WithStatusTraffic(v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						RevisionName:   "config-00001",
						Percent:        ptr.Int64(100),
						LatestRevision: ptr.Bool(true),
					},
				}), MarkCertificateNotReady),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created placeholder service %q", "becomes-ready"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Certificate %s/%s", "default", "route-12-34"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "becomes-ready"),
		},
		Key: "default/becomes-ready",
	}, {
		Name: "check that Certificate and IngressTLS are correctly updated when updating a Route",
		Objects: []runtime.Object{
			Route("default", "becomes-ready", WithConfigTarget("config"), WithRouteUID("12-34")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001")),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001"), WithServiceName("mcd")),
			// MakeCertificates will create a certificate with DNS name "*.test-ns.example.com" which is not the host name
			// needed by the input Route.
			&netv1alpha1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-12-34",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(
						Route("default", "becomes-ready", WithConfigTarget("config"), WithRouteUID("12-34")))},
					Annotations: map[string]string{
						networking.CertificateClassAnnotationKey: network.CertManagerCertificateClassName,
					},
					Labels: map[string]string{
						serving.RouteLabelKey: "becomes-ready",
					},
				},
				Spec: netv1alpha1.CertificateSpec{
					DNSNames: []string{"abc.test.example.com"},
				},
				Status: readyCertStatus(),
			},
		},
		WantCreates: []runtime.Object{
			ingressWithTLS(
				Route("default", "becomes-ready", WithConfigTarget("config"), WithURL,
					WithRouteUID("12-34")),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "mcd",
							Active:      true,
						}},
					},
				},
				[]netv1alpha1.IngressTLS{
					{
						Hosts:           []string{"becomes-ready.default.example.com"},
						SecretName:      "route-12-34",
						SecretNamespace: "default",
					},
				},
			),
			simpleK8sService(
				Route("default", "becomes-ready", WithConfigTarget("config"), WithRouteUID("12-34")),
				WithExternalName("becomes-ready.default.example.com"),
			),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: certificateWithStatus(resources.MakeCertificates(Route("default", "becomes-ready", WithConfigTarget("config"), WithURL, WithRouteUID("12-34")),
				map[string]string{"becomes-ready.default.example.com": ""}, network.CertManagerCertificateClassName)[0], readyCertStatus()),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "becomes-ready", WithConfigTarget("config"),
				WithRouteUID("12-34"),
				// Populated by reconciliation when all traffic has been assigned.
				WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressNotConfigured, WithStatusTraffic(v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						RevisionName:   "config-00001",
						Percent:        ptr.Int64(100),
						LatestRevision: ptr.Bool(true),
					},
				}), MarkCertificateReady,
				// The certificate is ready. So we want to have HTTPS URL.
				WithHTTPSDomain),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created placeholder service %q", "becomes-ready"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Spec for Certificate %s/%s", "default", "route-12-34"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "becomes-ready"),
		},
		Key: "default/becomes-ready",
	}, {
		Name:    "check that Route updates status and produces event log when valid name but not owned certificate",
		WantErr: true,
		Objects: []runtime.Object{
			Route("default", "becomes-ready", WithConfigTarget("config"), WithRouteUID("12-34")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001")),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001"), WithServiceName("mcd")),
			&netv1alpha1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-12-34",
					Namespace: "default",
					// Mark OwnerReferences for this test.
					OwnerReferences: nil,
					Annotations: map[string]string{
						networking.CertificateClassAnnotationKey: network.CertManagerCertificateClassName,
					},
					Labels: map[string]string{
						serving.RouteLabelKey: "becomes-ready",
					},
				},
				Spec: netv1alpha1.CertificateSpec{
					DNSNames: []string{"becomes-ready.default.example.com"},
				},
				Status: readyCertStatus(),
			},
		},
		WantDeleteCollections: []clientgotesting.DeleteCollectionActionImpl{},
		WantCreates: []runtime.Object{
			simplePlaceholderK8sService(getContext(), Route("default", "becomes-ready", WithConfigTarget("config"), WithRouteUID("12-34")), ""),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "becomes-ready", WithConfigTarget("config"),
				WithRouteUID("12-34"),
				WithAddress, WithInitRouteConditions, WithURL,
				MarkTrafficAssigned, WithStatusTraffic(v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						RevisionName:   "config-00001",
						Percent:        ptr.Int64(100),
						LatestRevision: ptr.Bool(true),
					},
				}), MarkCertificateNotOwned),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created placeholder service %q", "becomes-ready"),
			Eventf(corev1.EventTypeWarning, "InternalError", kaccessor.NewAccessorError(fmt.Errorf("owner: %s with Type %T does not own Certificate: %q", "becomes-ready", &v1alpha1.Route{}, "route-12-34"), kaccessor.NotOwnResource).Error()),
		},
		Key: "default/becomes-ready",
	}, {
		Name: "check that Route is correctly updated when Certificate is not ready",
		Objects: []runtime.Object{
			Route("default", "becomes-ready", WithConfigTarget("config"), WithRouteUID("12-34")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001")),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001"), WithServiceName("mcd")),
			// MakeCertificates will create a certificate with DNS name "*.test-ns.example.com" which is not the host name
			// needed by the input Route.
			&netv1alpha1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-12-34",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(
						Route("default", "becomes-ready", WithConfigTarget("config"), WithRouteUID("12-34")))},
					Labels: labels.Set(map[string]string{
						serving.RouteLabelKey: "becomes-ready",
					}),
					Annotations: map[string]string{
						networking.CertificateClassAnnotationKey: network.CertManagerCertificateClassName,
					},
				},
				Spec: netv1alpha1.CertificateSpec{
					DNSNames: []string{"abc.test.example.com"},
				},
				Status: notReadyCertStatus(),
			},
		},
		WantCreates: []runtime.Object{
			ingressWithTLS(
				Route("default", "becomes-ready", WithConfigTarget("config"), WithURL,
					WithRouteUID("12-34")),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "mcd",
							Active:      true,
						}},
					},
				},
				[]netv1alpha1.IngressTLS{
					{
						Hosts:           []string{"becomes-ready.default.example.com"},
						SecretName:      "route-12-34",
						SecretNamespace: "default",
					},
				},
			),
			simpleK8sService(
				Route("default", "becomes-ready", WithConfigTarget("config"), WithRouteUID("12-34")),
				WithExternalName("becomes-ready.default.example.com"),
			),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: certificateWithStatus(resources.MakeCertificates(Route("default", "becomes-ready", WithConfigTarget("config"), WithURL, WithRouteUID("12-34")),
				map[string]string{"becomes-ready.default.example.com": ""}, network.CertManagerCertificateClassName)[0], notReadyCertStatus()),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "becomes-ready", WithConfigTarget("config"),
				WithRouteUID("12-34"),
				// Populated by reconciliation when all traffic has been assigned.
				WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressNotConfigured, WithStatusTraffic(v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						RevisionName:   "config-00001",
						Percent:        ptr.Int64(100),
						LatestRevision: ptr.Bool(true),
					},
				}), MarkCertificateNotReady, MarkIngressNotConfigured,
				// The certificate is not ready. So we want to have HTTP URL.
				WithURL),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created placeholder service %q", "becomes-ready"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Spec for Certificate %s/%s", "default", "route-12-34"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "becomes-ready"),
		},
		Key: "default/becomes-ready",
	}, {
		// This test is a same with "public becomes cluster local" above, but confirm it does not create certs with autoTLS for cluster-local.
		Name: "public becomes cluster local w/ autoTLS",
		Objects: []runtime.Object{
			Route("default", "becomes-local", WithConfigTarget("config"),
				WithRouteLabel(map[string]string{config.VisibilityLabelKey: config.VisibilityClusterLocal}),
				WithRouteUID("65-23")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001")),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001"), WithServiceName("tb")),
			simpleIngress(
				Route("default", "becomes-local", WithConfigTarget("config"), WithRouteUID("65-23"), WithRouteLabel(map[string]string{"serving.knative.dev/visibility": "cluster-local"})),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "tb",
							Active:      true,
						}},
					},
				},
			),
			simpleK8sService(Route("default", "becomes-local", WithConfigTarget("config"),
				WithRouteLabel(map[string]string{config.VisibilityLabelKey: config.VisibilityClusterLocal}),
				WithRouteUID("65-23"))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleIngressWithVisibility(
				Route("default", "becomes-local", WithConfigTarget("config"),
					WithRouteUID("65-23"),
					WithRouteLabel(map[string]string{config.VisibilityLabelKey: config.VisibilityClusterLocal})),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "tb",
							Active:      true,
						}},
					},
				},
				sets.NewString("becomes-local"),
			),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "becomes-local", WithConfigTarget("config"),
				WithRouteUID("65-23"),
				MarkTrafficAssigned, MarkIngressNotConfigured,
				WithLocalDomain, WithAddress, WithInitRouteConditions,
				WithRouteLabel(map[string]string{config.VisibilityLabelKey: config.VisibilityClusterLocal}),
				WithStatusTraffic(v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						RevisionName:   "config-00001",
						Percent:        ptr.Int64(100),
						LatestRevision: ptr.Bool(true),
					},
				})),
		}},
		Key: "default/becomes-local",
	}}
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		return &Reconciler{
			Base:                reconciler.NewBase(ctx, controllerAgentName, cmw),
			routeLister:         listers.GetRouteLister(),
			configurationLister: listers.GetConfigurationLister(),
			revisionLister:      listers.GetRevisionLister(),
			serviceLister:       listers.GetK8sServiceLister(),
			ingressLister:       listers.GetIngressLister(),
			certificateLister:   listers.GetCertificateLister(),
			tracker:             &NullTracker{},
			configStore: &testConfigStore{
				config: ReconcilerTestConfig(true),
			},
			clock: FakeClock{Time: fakeCurTime},
		}
	}))
}

func wildcardCert(namespace string, domain string) *netv1alpha1.Certificate {
	cert := &netv1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      fmt.Sprintf("%s.%s", namespace, domain),
			Labels: map[string]string{
				networking.WildcardCertDomainLabelKey: domain,
			},
		},
		Spec: netv1alpha1.CertificateSpec{
			DNSNames:   []string{fmt.Sprintf("*.%s.%s", namespace, domain)},
			SecretName: namespace,
		},
		Status: readyCertStatus(),
	}

	return cert
}

func TestReconcile_EnableAutoTLS_HTTPDisabled(t *testing.T) {
	table := TableTest{{
		Name: "check that Route is correctly updated when Certificate is not ready",
		Objects: []runtime.Object{
			Route("default", "becomes-ready", WithConfigTarget("config"), WithRouteUID("12-34")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated("config-00001"), WithLatestReady("config-00001")),
			rev("default", "config", 1, MarkRevisionReady, WithRevName("config-00001"), WithServiceName("mcd")),
			// MakeCertificates will create a certificate with DNS name "*.test-ns.example.com" which is not the host name
			// needed by the input Route.
			&netv1alpha1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route-12-34",
					Namespace: "default",
					Labels: labels.Set(map[string]string{
						serving.RouteLabelKey: "becomes-ready",
					}),
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(
						Route("default", "becomes-ready", WithConfigTarget("config"), WithRouteUID("12-34")))},
					Annotations: map[string]string{
						networking.CertificateClassAnnotationKey: network.CertManagerCertificateClassName,
					},
				},
				Spec: netv1alpha1.CertificateSpec{
					DNSNames: []string{"abc.test.example.com"},
				},
				Status: notReadyCertStatus(),
			},
		},
		WantCreates: []runtime.Object{
			ingressWithTLS(
				Route("default", "becomes-ready", WithConfigTarget("config"), WithURL,
					WithRouteUID("12-34")),
				&traffic.Config{
					Targets: map[string]traffic.RevisionTargets{
						traffic.DefaultTarget: {{
							TrafficTarget: v1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: "config-00001",
								Percent:      ptr.Int64(100),
							},
							ServiceName: "mcd",
							Active:      true,
						}},
					},
				},
				[]netv1alpha1.IngressTLS{
					{
						Hosts:           []string{"becomes-ready.default.example.com"},
						SecretName:      "route-12-34",
						SecretNamespace: "default",
					},
				},
			),
			simpleK8sService(
				Route("default", "becomes-ready", WithConfigTarget("config"), WithRouteUID("12-34")),
				WithExternalName("becomes-ready.default.example.com"),
			),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: certificateWithStatus(resources.MakeCertificates(Route("default", "becomes-ready", WithConfigTarget("config"), WithURL, WithRouteUID("12-34")),
				map[string]string{"becomes-ready.default.example.com": ""}, network.CertManagerCertificateClassName)[0], notReadyCertStatus()),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Route("default", "becomes-ready", WithConfigTarget("config"),
				WithRouteUID("12-34"),
				// Populated by reconciliation when all traffic has been assigned.
				WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressNotConfigured, WithStatusTraffic(v1alpha1.TrafficTarget{
					TrafficTarget: v1.TrafficTarget{
						RevisionName:   "config-00001",
						Percent:        ptr.Int64(100),
						LatestRevision: ptr.Bool(true),
					},
				}), MarkCertificateNotReady, MarkIngressNotConfigured,
				// The certificate is not ready. But we still want to have HTTPS URL.
				WithHTTPSDomain),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created placeholder service %q", "becomes-ready"),
			Eventf(corev1.EventTypeNormal, "Updated", "Updated Spec for Certificate %s/%s", "default", "route-12-34"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "becomes-ready"),
		},
		Key: "default/becomes-ready",
	}}
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		cfg := ReconcilerTestConfig(true)
		cfg.Network.HTTPProtocol = network.HTTPDisabled
		return &Reconciler{
			Base:                reconciler.NewBase(ctx, controllerAgentName, cmw),
			routeLister:         listers.GetRouteLister(),
			configurationLister: listers.GetConfigurationLister(),
			revisionLister:      listers.GetRevisionLister(),
			serviceLister:       listers.GetK8sServiceLister(),
			ingressLister:       listers.GetIngressLister(),
			certificateLister:   listers.GetCertificateLister(),
			tracker:             &NullTracker{},
			configStore: &testConfigStore{
				config: cfg,
			},
			clock: FakeClock{Time: fakeCurTime},
		}
	}))
}

func cfg(namespace, name string, co ...ConfigOption) *v1alpha1.Configuration {
	cfg := &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            name,
			ResourceVersion: "v1",
		},
		Spec: v1alpha1.ConfigurationSpec{
			Template: &v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					RevisionSpec: v1.RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "busybox",
							}},
						},
					},
				},
			},
		},
	}
	for _, opt := range co {
		opt(cfg)
	}
	return cfg
}

func simplePlaceholderK8sService(ctx context.Context, r *v1alpha1.Route, targetName string, so ...K8sServiceOption) *corev1.Service {
	// omit the error here, as we are sure the loadbalancer info is porvided.
	// return the service instance only, so that the result can be used in TableRow.
	svc, _ := resources.MakeK8sPlaceholderService(ctx, r, targetName)

	for _, opt := range so {
		opt(svc)
	}

	return svc
}

func simpleK8sService(r *v1alpha1.Route, so ...K8sServiceOption) *corev1.Service {
	cs := &testConfigStore{
		config: ReconcilerTestConfig(false),
	}
	ctx := cs.ToContext(context.Background())

	// omit the error here, as we are sure the loadbalancer info is porvided.
	// return the service instance only, so that the result can be used in TableRow.
	svc, _ := resources.MakeK8sService(ctx, r, "", &netv1alpha1.Ingress{Status: readyIngressStatus()}, false)

	for _, opt := range so {
		opt(svc)
	}

	return svc
}

type ingressCtor func(ctx context.Context,
	r *v1alpha1.Route,
	tc *traffic.Config,
	tls []netv1alpha1.IngressTLS,
	clusterLocalServices sets.String,
	ingressClass string) (netv1alpha1.IngressAccessor, error)

func simpleIngress(r *v1alpha1.Route, tc *traffic.Config, io ...IngressOption) netv1alpha1.IngressAccessor {
	return simpleIngressWithVisibility(r, tc, sets.NewString(), io...)
}

func simpleIngressWithVisibility(r *v1alpha1.Route, tc *traffic.Config, serviceVisibility sets.String, io ...IngressOption) netv1alpha1.IngressAccessor {
	return baseIngressWithClass(r, tc, TestIngressClass, serviceVisibility, resources.MakeIngress, io...)
}

func ingressWithClass(r *v1alpha1.Route, tc *traffic.Config, class string, serviceVisibility sets.String, io ...IngressOption) netv1alpha1.IngressAccessor {
	return baseIngressWithClass(r, tc, class, serviceVisibility, resources.MakeIngress, io...)
}

func baseIngressWithClass(r *v1alpha1.Route, tc *traffic.Config, class string, serviceVisibility sets.String, ctor ingressCtor, io ...IngressOption) netv1alpha1.IngressAccessor {
	ingress, _ := ctor(getContext(), r, tc, nil, serviceVisibility, class)

	for _, opt := range io {
		opt(ingress)
	}

	return ingress
}

func ingressWithTLS(r *v1alpha1.Route, tc *traffic.Config, tls []netv1alpha1.IngressTLS, io ...IngressOption) netv1alpha1.IngressAccessor {
	return baseIngressWithTLS(r, tc, tls, resources.MakeIngress, io...)
}

func baseIngressWithTLS(r *v1alpha1.Route, tc *traffic.Config, tls []netv1alpha1.IngressTLS, ctor ingressCtor, io ...IngressOption) netv1alpha1.IngressAccessor {
	ingress, _ := ctor(getContext(), r, tc, tls, sets.NewString(), TestIngressClass)

	for _, opt := range io {
		opt(ingress)
	}

	return ingress
}

func simpleReadyIngress(r *v1alpha1.Route, tc *traffic.Config, io ...IngressOption) netv1alpha1.IngressAccessor {
	ingress := ingressWithStatus(r, tc, readyIngressStatus())

	for _, opt := range io {
		opt(ingress)
	}

	return ingress
}

func readyIngressStatus() netv1alpha1.IngressStatus {
	status := netv1alpha1.IngressStatus{}
	status.InitializeConditions()
	status.MarkNetworkConfigured()
	status.MarkLoadBalancerReady(
		[]netv1alpha1.LoadBalancerIngressStatus{
			{DomainInternal: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system")},
		},
		[]netv1alpha1.LoadBalancerIngressStatus{
			{DomainInternal: pkgnet.GetServiceHostname("istio-ingressgateway", "istio-system")},
		},
		[]netv1alpha1.LoadBalancerIngressStatus{
			{DomainInternal: pkgnet.GetServiceHostname("private-istio-ingressgateway", "istio-system")},
		},
	)

	return status
}

func ingressWithStatus(r *v1alpha1.Route, tc *traffic.Config, status netv1alpha1.IngressStatus) netv1alpha1.IngressAccessor {
	ci := simpleIngress(r, tc)
	ci.SetName(r.Name)
	ci.SetStatus(status)

	return ci
}

func mutateIngress(ci netv1alpha1.IngressAccessor) netv1alpha1.IngressAccessor {
	// Thor's Hammer
	ci.SetSpec(netv1alpha1.IngressSpec{})
	return ci
}

func patchLastPinned(namespace, name string) clientgotesting.PatchActionImpl {
	action := clientgotesting.PatchActionImpl{}
	action.Name = name
	action.Namespace = namespace
	lastPinStr := v1alpha1.RevisionLastPinnedString(fakeCurTime)
	patch := fmt.Sprintf(`{"metadata":{"annotations":{"serving.knative.dev/lastPinned":%q}}}`, lastPinStr)
	action.Patch = []byte(patch)
	return action
}

func rev(namespace, name string, generation int64, ro ...RevisionOption) *v1alpha1.Revision {
	r := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Annotations: map[string]string{
				"serving.knative.dev/lastPinned": v1alpha1.RevisionLastPinnedString(
					fakeCurTime.Add(-1 * time.Second)),
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         v1alpha1.SchemeGroupVersion.String(),
				Kind:               "Configuration",
				Name:               name,
				Controller:         ptr.Bool(true),
				BlockOwnerDeletion: ptr.Bool(true),
			}},
		},
	}
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

var _ reconciler.ConfigStore = (*testConfigStore)(nil)

func ReconcilerTestConfig(enableAutoTLS bool) *config.Config {
	return &config.Config{
		Domain: &config.Domain{
			Domains: map[string]*config.LabelSelector{
				"example.com": {},
				"another-example.com": {
					Selector: map[string]string{"app": "prod"},
				},
			},
		},
		Network: &network.Config{
			DefaultIngressClass:     TestIngressClass,
			DefaultCertificateClass: network.CertManagerCertificateClassName,
			AutoTLS:                 enableAutoTLS,
			DomainTemplate:          network.DefaultDomainTemplate,
			TagTemplate:             network.DefaultTagTemplate,
			HTTPProtocol:            network.HTTPEnabled,
		},
		GC: &gc.Config{
			StaleRevisionLastpinnedDebounce: time.Duration(1 * time.Minute),
		},
	}
}

func readyCertStatus() netv1alpha1.CertificateStatus {
	certStatus := &netv1alpha1.CertificateStatus{}
	certStatus.MarkReady()
	return *certStatus
}

func notReadyCertStatus() netv1alpha1.CertificateStatus {
	certStatus := &netv1alpha1.CertificateStatus{}
	certStatus.MarkNotReady("not ready", "not ready")
	return *certStatus
}

func certificateWithStatus(cert *netv1alpha1.Certificate, status netv1alpha1.CertificateStatus) *netv1alpha1.Certificate {
	cert.Status = status
	return cert
}
