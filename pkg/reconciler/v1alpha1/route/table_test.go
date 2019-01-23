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

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	netv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/gc"
	"github.com/knative/serving/pkg/reconciler"
	rtesting "github.com/knative/serving/pkg/reconciler/testing"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/config"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/resources"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/traffic"
	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"
)

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
			route("default", "first-reconcile", WithConfigTarget("not-ready")),
			cfg("default", "not-ready", WithGeneration(1), WithLatestCreated),
			rev("default", "not-ready", 1, WithInitRevConditions),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "first-reconcile", WithConfigTarget("not-ready"),
				// The first reconciliation initializes the conditions and reflects
				// that the referenced configuration is not yet ready.
				WithInitRouteConditions, MarkConfigurationNotReady("not-ready")),
		}},
		Key: "default/first-reconcile",
	}, {
		Name: "configuration permanently failed",
		Objects: []runtime.Object{
			route("default", "first-reconcile", WithConfigTarget("permanently-failed")),
			cfg("default", "permanently-failed",
				WithGeneration(1), WithLatestCreated, MarkLatestCreatedFailed("blah")),
			rev("default", "permanently-failed", 1, WithInitRevConditions, MarkContainerMissing),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "first-reconcile", WithConfigTarget("permanently-failed"),
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
			route("default", "first-reconcile", WithConfigTarget("not-ready")),
			cfg("default", "not-ready", WithGeneration(1), WithLatestCreated),
			rev("default", "not-ready", 1, WithInitRevConditions),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "first-reconcile", WithConfigTarget("not-ready"),
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
			route("default", "becomes-ready", WithConfigTarget("config")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated, WithLatestReady),
			rev("default", "config", 1, MarkRevisionReady),
		},
		WantCreates: []metav1.Object{
			resources.MakeClusterIngress(
				route("default", "becomes-ready", WithConfigTarget("config"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "becomes-ready", WithConfigTarget("config"),
				// Populated by reconciliation when all traffic has been assigned.
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, WithStatusTraffic(v1alpha1.TrafficTarget{
					RevisionName: "config-00001",
					Percent:      100,
				})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created ClusterIngress %q", ""),
		},
		Key: "default/becomes-ready",
		// TODO(lichuqiang): config namespace validation in resource scope.
		SkipNamespaceValidation: true,
	}, {
		Name: "cluster local route becomes ready, ingress unknown",
		Objects: []runtime.Object{
			route("default", "becomes-ready", WithConfigTarget("config"), WithLocalDomain,
				WithRouteLabel("serving.knative.dev/visibility", "cluster-local")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated, WithLatestReady),
			rev("default", "config", 1, MarkRevisionReady),
		},
		WantCreates: []metav1.Object{
			resources.MakeClusterIngress(
				route("default", "becomes-ready", WithConfigTarget("config"), WithLocalDomain,
					WithRouteLabel("serving.knative.dev/visibility", "cluster-local")),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "becomes-ready", WithConfigTarget("config"),
				// Populated by reconciliation when all traffic has been assigned.
				WithLocalDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				WithRouteLabel("serving.knative.dev/visibility", "cluster-local"),
				MarkTrafficAssigned, WithStatusTraffic(v1alpha1.TrafficTarget{
					RevisionName: "config-00001",
					Percent:      100,
				})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created ClusterIngress %q", ""),
		},
		Key: "default/becomes-ready",
		// TODO(lichuqiang): config namespace validation in resource scope.
		SkipNamespaceValidation: true,
	}, {
		Name: "simple route becomes ready",
		Objects: []runtime.Object{
			route("default", "becomes-ready", WithConfigTarget("config")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated, WithLatestReady),
			rev("default", "config", 1, MarkRevisionReady),
			simpleReadyIngress(
				route("default", "becomes-ready", WithConfigTarget("config"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
		},
		WantCreates: []metav1.Object{
			simpleK8sService(route("default", "becomes-ready", WithConfigTarget("config"))),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "becomes-ready", WithConfigTarget("config"),
				// Populated by reconciliation when the route becomes ready.
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "config-00001",
						Percent:      100,
					})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created service %q", "becomes-ready"),
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
			route("default", "create-svc-failure", WithConfigTarget("config")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated, WithLatestReady),
			rev("default", "config", 1, MarkRevisionReady),
			simpleReadyIngress(
				route("default", "create-svc-failure", WithConfigTarget("config"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
		},
		WantCreates: []metav1.Object{
			simpleK8sService(route("default", "create-svc-failure", WithConfigTarget("config"))),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "create-svc-failure", WithConfigTarget("config"),
				// Populated by reconciliation when we've failed to create
				// the K8s service.
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "config-00001",
						Percent:      100,
					})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "CreationFailed", "Failed to create service %q: %v",
				"create-svc-failure", "inducing failure for create services"),
		},
		Key: "default/create-svc-failure",
	}, {
		Name: "failure creating cluster ingress",
		Objects: []runtime.Object{
			route("default", "ingress-create-failure", WithConfigTarget("config")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated, WithLatestReady),
			rev("default", "config", 1, MarkRevisionReady),
		},
		// We induce a failure creating the ClusterIngress.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "clusteringresses"),
		},
		WantCreates: []metav1.Object{
			// This is the Create we see for the cluter ingress, but we induce a failure.
			resources.MakeClusterIngress(
				route("default", "ingress-create-failure", WithConfigTarget("config"),
					WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "ingress-create-failure", WithConfigTarget("config"),
				// Populated by reconciliation when we fail to create
				// the cluster ingress.
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, WithStatusTraffic(v1alpha1.TrafficTarget{
					RevisionName: "config-00001",
					Percent:      100,
				})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "CreationFailed", "Failed to create ClusterIngress for route %s/%s: %v",
				"default", "ingress-create-failure", "inducing failure for create clusteringresses"),
		},
		Key:                     "default/ingress-create-failure",
		SkipNamespaceValidation: true,
	}, {
		Name: "steady state",
		Objects: []runtime.Object{
			route("default", "steady-state", WithConfigTarget("config"),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "config-00001",
						Percent:      100,
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated, WithLatestReady,
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "steady-state"),
			),
			rev("default", "config", 1, MarkRevisionReady),
			simpleReadyIngress(
				route("default", "steady-state", WithConfigTarget("config"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(route("default", "steady-state", WithConfigTarget("config"))),
		},
		Key: "default/steady-state",
	}, {
		Name:    "unhappy about ownership of placeholder service",
		WantErr: true,
		Objects: []runtime.Object{
			route("default", "unhappy-owner", WithConfigTarget("config"),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "config-00001",
						Percent:      100,
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated, WithLatestReady,
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "unhappy-owner"),
			),
			rev("default", "config", 1, MarkRevisionReady),
			simpleReadyIngress(
				route("default", "unhappy-owner", WithConfigTarget("config"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(route("default", "unhappy-owner", WithConfigTarget("config")),
				WithK8sSvcOwnersRemoved),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "unhappy-owner", WithConfigTarget("config"),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "config-00001",
						Percent:      100,
					}),
				// The owner is not us, so we are unhappy.
				MarkServiceNotOwned),
		}},
		Key: "default/unhappy-owner",
	}, {
		// This tests that when the Route is labelled differently, it is configured with a
		// different domain from config-domain.yaml.  This is otherwise a copy of the steady
		// state test above.
		Name: "different labels, different domain - steady state",
		Objects: []runtime.Object{
			route("default", "different-domain", WithConfigTarget("config"),
				WithAnotherDomain, WithDomainInternal, WithAddress,
				WithInitRouteConditions, MarkTrafficAssigned, MarkIngressReady,
				WithStatusTraffic(v1alpha1.TrafficTarget{
					RevisionName: "config-00001",
					Percent:      100,
				}), WithRouteLabel("app", "prod")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated, WithLatestReady,
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "different-domain"),
			),
			rev("default", "config", 1, MarkRevisionReady),
			simpleReadyIngress(
				route("default", "different-domain", WithConfigTarget("config"),
					WithAnotherDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(route("default", "different-domain", WithConfigTarget("config"))),
		},
		Key: "default/different-domain",
	}, {
		Name: "new latest created revision",
		Objects: []runtime.Object{
			route("default", "new-latest-created", WithConfigTarget("config"),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "config-00001",
						Percent:      100,
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated, WithLatestReady,
				WithGeneration(2), WithLatestCreated,
				WithConfigLabel("serving.knative.dev/route", "new-latest-created"),
			),
			rev("default", "config", 1, MarkRevisionReady),
			// This is the name of the new revision we're referencing above.
			rev("default", "config", 2, WithInitRevConditions),
			simpleReadyIngress(
				route("default", "new-latest-created", WithConfigTarget("config"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(route("default", "new-latest-created", WithConfigTarget("config"))),
		},
		// A new LatestCreatedRevisionName on the Configuration alone should result in no changes to the Route.
		Key: "default/new-latest-created",
	}, {
		Name: "new latest ready revision",
		Objects: []runtime.Object{
			route("default", "new-latest-ready", WithConfigTarget("config"),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "config-00001",
						Percent:      100,
					})),
			cfg("default", "config",
				WithGeneration(2), WithLatestCreated, WithLatestReady,
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "new-latest-ready"),
			),
			rev("default", "config", 1, MarkRevisionReady),
			// This is the name of the new revision we're referencing above.
			rev("default", "config", 2, MarkRevisionReady),
			simpleReadyIngress(
				route("default", "new-latest-ready", WithConfigTarget("config"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(route("default", "new-latest-ready", WithConfigTarget("config"))),
		},
		// A new LatestReadyRevisionName on the Configuration should result in the new Revision being rolled out.
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleReadyIngress(
				route("default", "new-latest-ready", WithConfigTarget("config"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// This is the new config we're making become ready.
								RevisionName: "config-00002",
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "new-latest-ready", WithConfigTarget("config"),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "config-00002",
						Percent:      100,
					})),
		}},
		Key:                     "default/new-latest-ready",
		SkipNamespaceValidation: true,
	}, {
		Name: "failure updating cluster ingress",
		// Starting from the new latest ready, induce a failure updating the cluster ingress.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "clusteringresses"),
		},
		Objects: []runtime.Object{
			route("default", "update-ci-failure", WithConfigTarget("config"),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "config-00001",
						Percent:      100,
					})),
			cfg("default", "config",
				WithGeneration(2), WithLatestCreated, WithLatestReady,
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "update-ci-failure"),
			),
			rev("default", "config", 1, MarkRevisionReady),
			// This is the name of the new revision we're referencing above.
			rev("default", "config", 2, MarkRevisionReady),
			simpleReadyIngress(
				route("default", "update-ci-failure", WithConfigTarget("config"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(route("default", "update-ci-failure", WithConfigTarget("config"))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleReadyIngress(
				route("default", "update-ci-failure", WithConfigTarget("config"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// This is the new config we're making become ready.
								RevisionName: "config-00002",
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "update-ci-failure", WithConfigTarget("config"),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "config-00002",
						Percent:      100,
					})),
		}},
		Key:                     "default/update-ci-failure",
		SkipNamespaceValidation: true,
	}, {
		Name: "reconcile service mutation",
		Objects: []runtime.Object{
			route("default", "svc-mutation", WithConfigTarget("config"),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "config-00001",
						Percent:      100,
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated, WithLatestReady,
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "svc-mutation"),
			),
			rev("default", "config", 1, MarkRevisionReady),
			simpleReadyIngress(
				route("default", "svc-mutation", WithConfigTarget("config"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(route("default", "svc-mutation",
				WithConfigTarget("config")), MutateK8sService),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleK8sService(route("default", "svc-mutation", WithConfigTarget("config"))),
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
			route("default", "svc-mutation", WithConfigTarget("config"),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "config-00001",
						Percent:      100,
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated, WithLatestReady,
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "svc-mutation"),
			),
			rev("default", "config", 1, MarkRevisionReady),
			simpleReadyIngress(
				route("default", "svc-mutation", WithConfigTarget("config"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(route("default", "svc-mutation",
				WithConfigTarget("config")), MutateK8sService),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleK8sService(route("default", "svc-mutation", WithConfigTarget("config"))),
		}},
		Key: "default/svc-mutation",
	}, {
		// In #1789 we switched this to an ExternalName Service. Services created in
		// 0.1 will still have ClusterIP set, which is Forbidden for ExternalName
		// Services. Ensure that we drop the ClusterIP if it is set in the spec.
		Name: "drop cluster ip",
		Objects: []runtime.Object{
			route("default", "cluster-ip", WithConfigTarget("config"),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "config-00001",
						Percent:      100,
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated, WithLatestReady,
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "cluster-ip"),
			),
			rev("default", "config", 1, MarkRevisionReady),
			simpleReadyIngress(
				route("default", "cluster-ip", WithConfigTarget("config"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(route("default", "cluster-ip",
				WithConfigTarget("config")), WithClusterIP("127.0.0.1")),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleK8sService(route("default", "cluster-ip", WithConfigTarget("config"))),
		}},
		Key: "default/cluster-ip",
	}, {
		// Make sure we fix the external name if something messes with it.
		Name: "fix external name",
		Objects: []runtime.Object{
			route("default", "external-name", WithConfigTarget("config"),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "config-00001",
						Percent:      100,
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated, WithLatestReady,
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "external-name"),
			),
			rev("default", "config", 1, MarkRevisionReady),
			simpleReadyIngress(
				route("default", "external-name", WithConfigTarget("config"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(route("default", "external-name",
				WithConfigTarget("config")), WithExternalName("this-is-the-wrong-name")),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleK8sService(route("default", "external-name", WithConfigTarget("config"))),
		}},
		Key: "default/external-name",
	}, {
		Name: "reconcile cluster ingress mutation",
		Objects: []runtime.Object{
			route("default", "ingress-mutation", WithConfigTarget("config"),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "config-00001",
						Percent:      100,
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated, WithLatestReady,
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "ingress-mutation"),
			),
			rev("default", "config", 1, MarkRevisionReady),
			mutateIngress(simpleReadyIngress(
				route("default", "ingress-mutation", WithConfigTarget("config"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			)),
			simpleK8sService(route("default", "ingress-mutation", WithConfigTarget("config"))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleReadyIngress(
				route("default", "ingress-mutation", WithConfigTarget("config"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
		}},
		Key:                     "default/ingress-mutation",
		SkipNamespaceValidation: true,
	}, {
		Name: "switch to a different config",
		Objects: []runtime.Object{
			// The status reflects "oldconfig", but the spec "newconfig".
			route("default", "change-configs", WithConfigTarget("newconfig"),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "oldconfig-00001",
						Percent:      100,
					})),
			// Both configs exist, but only "oldconfig" is labelled.
			cfg("default", "oldconfig",
				WithGeneration(1), WithLatestCreated, WithLatestReady,
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "change-configs"),
			),
			cfg("default", "newconfig",
				WithGeneration(1), WithLatestCreated, WithLatestReady),
			rev("default", "oldconfig", 1, MarkRevisionReady),
			rev("default", "newconfig", 1, MarkRevisionReady),
			simpleReadyIngress(
				route("default", "change-configs", WithConfigTarget("oldconfig"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "oldconfig", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(route("default", "change-configs", WithConfigTarget("oldconfig"))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// Updated to point to "newconfig" things.
			Object: simpleReadyIngress(
				route("default", "change-configs", WithConfigTarget("newconfig"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "newconfig", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			// Status updated to "newconfig"
			Object: route("default", "change-configs", WithConfigTarget("newconfig"),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "newconfig-00001",
						Percent:      100,
					})),
		}},
		Key: "default/change-configs",
	}, {
		Name: "configuration missing",
		Objects: []runtime.Object{
			route("default", "config-missing", WithConfigTarget("not-found")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "config-missing", WithConfigTarget("not-found"),
				WithInitRouteConditions, MarkMissingTrafficTarget("Configuration", "not-found")),
		}},
		Key: "default/config-missing",
	}, {
		Name: "revision missing (direct)",
		Objects: []runtime.Object{
			route("default", "missing-revision-direct", WithRevTarget("not-found")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated, WithLatestReady),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "missing-revision-direct", WithRevTarget("not-found"),
				WithInitRouteConditions, MarkMissingTrafficTarget("Revision", "not-found")),
		}},
		Key: "default/missing-revision-direct",
	}, {
		Name: "revision missing (indirect)",
		Objects: []runtime.Object{
			route("default", "missing-revision-indirect", WithConfigTarget("config")),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated, WithLatestReady),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "missing-revision-indirect", WithConfigTarget("config"),
				WithInitRouteConditions, MarkMissingTrafficTarget("Revision", "config-00001")),
		}},
		Key: "default/missing-revision-indirect",
	}, {
		Name: "pinned route becomes ready",
		Objects: []runtime.Object{
			route("default", "pinned-becomes-ready", WithRevTarget(
				// Use the Revision name from the config
				rev("default", "config", 1).Name)),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated, WithLatestReady),
			rev("default", "config", 1, MarkRevisionReady),
			simpleK8sService(route("default", "pinned-becomes-ready", WithConfigTarget("config"))),
			simpleReadyIngress(
				route("default", "pinned-becomes-ready", WithConfigTarget("config"),
					WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "pinned-becomes-ready",
				// Use the Revision name from the config
				WithRevTarget(rev("default", "config", 1).Name),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: rev("default", "config", 1).Name,
						Percent:      100,
					})),
		}},
		Key:                     "default/pinned-becomes-ready",
		SkipNamespaceValidation: true,
	}, {
		Name: "traffic split becomes ready",
		Objects: []runtime.Object{
			route("default", "named-traffic-split", WithSpecTraffic(
				v1alpha1.TrafficTarget{
					ConfigurationName: "blue",
					Percent:           50,
				}, v1alpha1.TrafficTarget{
					ConfigurationName: "green",
					Percent:           50,
				})),
			cfg("default", "blue",
				WithGeneration(1), WithLatestCreated, WithLatestReady),
			cfg("default", "green",
				WithGeneration(1), WithLatestCreated, WithLatestReady),
			rev("default", "blue", 1, MarkRevisionReady),
			rev("default", "green", 1, MarkRevisionReady),
		},
		WantCreates: []metav1.Object{
			resources.MakeClusterIngress(
				route("default", "named-traffic-split", WithDomain, WithSpecTraffic(
					v1alpha1.TrafficTarget{
						ConfigurationName: "blue",
						Percent:           50,
					}, v1alpha1.TrafficTarget{
						ConfigurationName: "green",
						Percent:           50,
					})),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "blue", 1).Name,
								Percent:      50,
							},
							Active: true,
						}, {
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "green", 1).Name,
								Percent:      50,
							},
							Active: true,
						}},
					},
				},
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "named-traffic-split",
				WithSpecTraffic(v1alpha1.TrafficTarget{
					ConfigurationName: "blue",
					Percent:           50,
				}, v1alpha1.TrafficTarget{
					ConfigurationName: "green",
					Percent:           50,
				}),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "blue-00001",
						Percent:      50,
					}, v1alpha1.TrafficTarget{
						RevisionName: "green-00001",
						Percent:      50,
					})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created ClusterIngress %q", ""),
		},
		Key:                     "default/named-traffic-split",
		SkipNamespaceValidation: true,
	}, {
		Name: "same revision targets",
		Objects: []runtime.Object{
			route("default", "same-revision-targets", WithSpecTraffic(
				v1alpha1.TrafficTarget{
					Name:              "gray",
					ConfigurationName: "gray",
					Percent:           50,
				}, v1alpha1.TrafficTarget{
					Name:         "also-gray",
					RevisionName: "gray-00001",
					Percent:      50,
				})),
			cfg("default", "gray",
				WithGeneration(1), WithLatestCreated, WithLatestReady),
			rev("default", "gray", 1, MarkRevisionReady),
		},
		WantCreates: []metav1.Object{
			resources.MakeClusterIngress(
				route("default", "same-revision-targets", WithDomain, WithSpecTraffic(
					v1alpha1.TrafficTarget{
						Name:              "gray",
						ConfigurationName: "gray",
						Percent:           50,
					}, v1alpha1.TrafficTarget{
						Name:         "also-gray",
						RevisionName: "gray-00001",
						Percent:      50,
					})),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "gray", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
						"gray": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "gray", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
						"also-gray": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "gray", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "same-revision-targets",
				WithSpecTraffic(v1alpha1.TrafficTarget{
					Name:              "gray",
					ConfigurationName: "gray",
					Percent:           50,
				}, v1alpha1.TrafficTarget{
					Name:         "also-gray",
					RevisionName: "gray-00001",
					Percent:      50,
				}),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						Name:         "gray",
						RevisionName: "gray-00001",
						Percent:      50,
					}, v1alpha1.TrafficTarget{
						Name:         "also-gray",
						RevisionName: "gray-00001",
						Percent:      50,
					})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created ClusterIngress %q", ""),
		},
		Key:                     "default/same-revision-targets",
		SkipNamespaceValidation: true,
	}, {
		Name: "change route configuration",
		// Start from a steady state referencing "blue", and modify the route spec to point to "green" instead.
		Objects: []runtime.Object{
			route("default", "switch-configs", WithConfigTarget("green"),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						Name:         "blue",
						RevisionName: "blue-00001",
						Percent:      100,
					})),
			cfg("default", "blue",
				WithGeneration(1), WithLatestCreated, WithLatestReady,
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "switch-configs"),
			),
			cfg("default", "green",
				WithGeneration(1), WithLatestCreated, WithLatestReady),
			rev("default", "blue", 1, MarkRevisionReady),
			rev("default", "green", 1, MarkRevisionReady),
			simpleReadyIngress(
				route("default", "switch-configs", WithConfigTarget("blue"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "blue", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(route("default", "switch-configs", WithConfigTarget("blue"))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleReadyIngress(
				route("default", "switch-configs", WithConfigTarget("green"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "green", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "switch-configs", WithConfigTarget("green"),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "green-00001",
						Percent:      100,
					})),
		}},
		Key:                     "default/switch-configs",
		SkipNamespaceValidation: true,
	}, {
		Name: "update single target to traffic split with unready revision",
		// Start from a steady state referencing "blue", and modify the route spec to point to both
		// "blue" and "green" instead, while "green" is not ready.
		Objects: []runtime.Object{
			route("default", "split", WithDomain, WithDomainInternal, WithAddress,
				WithInitRouteConditions, MarkTrafficAssigned, MarkIngressReady,
				WithSpecTraffic(
					v1alpha1.TrafficTarget{
						ConfigurationName: "blue",
						Percent:           50,
					}, v1alpha1.TrafficTarget{
						ConfigurationName: "green",
						Percent:           50,
					}),
				WithStatusTraffic(
					v1alpha1.TrafficTarget{
						ConfigurationName: "blue",
						Percent:           100,
					},
				)),
			cfg("default", "blue", WithGeneration(1), WithLatestCreated, WithLatestReady),
			cfg("default", "green", WithGeneration(1), WithLatestCreated),
			rev("default", "blue", 1, MarkRevisionReady),
			rev("default", "green", 1),
			simpleReadyIngress(
				route("default", "split", WithConfigTarget("blue"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "blue", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(route("default", "split", WithConfigTarget("blue"))),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("default", "split", WithDomain, WithDomainInternal, WithAddress,
				WithInitRouteConditions, MarkTrafficAssigned, MarkIngressReady,
				MarkConfigurationNotReady("green"),
				WithSpecTraffic(
					v1alpha1.TrafficTarget{
						ConfigurationName: "blue",
						Percent:           50,
					}, v1alpha1.TrafficTarget{
						ConfigurationName: "green",
						Percent:           50,
					}),
				WithStatusTraffic(
					v1alpha1.TrafficTarget{
						ConfigurationName: "blue",
						Percent:           100,
					})),
		}},
		Key:                     "default/split",
		SkipNamespaceValidation: true,
	}, {
		Name: "Update stale lastPinned",
		Objects: []runtime.Object{
			route("default", "stale-lastpinned", WithConfigTarget("config"),
				WithDomain, WithDomainInternal, WithAddress, WithInitRouteConditions,
				MarkTrafficAssigned, MarkIngressReady, WithStatusTraffic(
					v1alpha1.TrafficTarget{
						RevisionName: "config-00001",
						Percent:      100,
					})),
			cfg("default", "config",
				WithGeneration(1), WithLatestCreated, WithLatestReady,
				// The Route controller attaches our label to this Configuration.
				WithConfigLabel("serving.knative.dev/route", "stale-lastpinned"),
			),
			rev("default", "config", 1, MarkRevisionReady,
				WithLastPinned(fakeCurTime.Add(-10*time.Minute))),
			simpleReadyIngress(
				route("default", "stale-lastpinned", WithConfigTarget("config"), WithDomain),
				&traffic.Config{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: rev("default", "config", 1).Name,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			simpleK8sService(route("default", "stale-lastpinned", WithConfigTarget("config"))),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchLastPinned("default", "config-00001"),
		},
		Key: "default/stale-lastpinned",
	}}

	// TODO(mattmoor): Revision inactive (direct reference)
	// TODO(mattmoor): Revision inactive (indirect reference)
	// TODO(mattmoor): Multiple inactive Revisions

	table.Test(t, MakeFactory(func(listers *Listers, opt reconciler.Options) controller.Reconciler {
		return &Reconciler{
			Base:                 reconciler.NewBase(opt, controllerAgentName),
			routeLister:          listers.GetRouteLister(),
			configurationLister:  listers.GetConfigurationLister(),
			revisionLister:       listers.GetRevisionLister(),
			serviceLister:        listers.GetK8sServiceLister(),
			clusterIngressLister: listers.GetClusterIngressLister(),
			tracker:              &rtesting.NullTracker{},
			configStore: &testConfigStore{
				config: ReconcilerTestConfig(),
			},
			clock: FakeClock{Time: fakeCurTime},
		}
	}))
}

func route(namespace, name string, ro ...RouteOption) *v1alpha1.Route {
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
	for _, opt := range ro {
		opt(r)
	}
	return r
}

func cfg(namespace, name string, co ...ConfigOption) *v1alpha1.Configuration {
	cfg := &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            name,
			ResourceVersion: "v1",
		},
		Spec: v1alpha1.ConfigurationSpec{
			DeprecatedGeneration: 1,
			RevisionTemplate: v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					Container: corev1.Container{
						Image: "busybox",
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

func simpleK8sService(r *v1alpha1.Route, so ...K8sServiceOption) *corev1.Service {
	// omit the error here, as we are sure the loadbalancer info is porvided.
	// return the service instance only, so that the result can be used in TableRow.
	svc, _ := resources.MakeK8sService(r, &netv1alpha1.ClusterIngress{Status: readyIngressStatus()})

	for _, opt := range so {
		opt(svc)
	}

	return svc
}

func simpleReadyIngress(r *v1alpha1.Route, tc *traffic.Config) *netv1alpha1.ClusterIngress {
	return ingressWithStatus(r, tc, readyIngressStatus())
}

func readyIngressStatus() netv1alpha1.IngressStatus {
	status := netv1alpha1.IngressStatus{}
	status.InitializeConditions()
	status.MarkNetworkConfigured()
	status.MarkLoadBalancerReady([]netv1alpha1.LoadBalancerIngressStatus{
		{DomainInternal: reconciler.GetK8sServiceFullname("istio-ingressgateway", "istio-system")},
	})

	return status
}

func ingressWithStatus(r *v1alpha1.Route, tc *traffic.Config, status netv1alpha1.IngressStatus) *netv1alpha1.ClusterIngress {
	ci := resources.MakeClusterIngress(r, tc)
	ci.Status = status

	return ci
}

func mutateIngress(ci *netv1alpha1.ClusterIngress) *netv1alpha1.ClusterIngress {
	// Thor's Hammer
	ci.Spec = netv1alpha1.IngressSpec{}
	return ci
}

func patchRemoveLabel(namespace, name, key, version string) clientgotesting.PatchActionImpl {
	action := clientgotesting.PatchActionImpl{}
	action.Name = name
	action.Namespace = namespace

	patch := fmt.Sprintf(`{"metadata":{"labels":{"%s":null},"resourceVersion":"%s"}}`, key, version)

	action.Patch = []byte(patch)
	return action
}

func patchAddLabel(namespace, name, key, value, version string) clientgotesting.PatchActionImpl {
	action := clientgotesting.PatchActionImpl{}
	action.Name = name
	action.Namespace = namespace

	patch := fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"},"resourceVersion":"%s"}}`, key, value, version)

	action.Patch = []byte(patch)
	return action
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
	c := cfg(namespace, name, WithGeneration(generation), WithLatestCreated)
	boolTrue := true
	r := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      c.Status.LatestCreatedRevisionName,
			Annotations: map[string]string{
				"serving.knative.dev/lastPinned": v1alpha1.RevisionLastPinnedString(
					fakeCurTime.Add(-1 * time.Second)),
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         v1alpha1.SchemeGroupVersion.String(),
				Kind:               "Configuration",
				Name:               name,
				Controller:         &boolTrue,
				BlockOwnerDeletion: &boolTrue,
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

func (t *testConfigStore) WatchConfigs(w configmap.Watcher) {}

var _ configStore = (*testConfigStore)(nil)

func ReconcilerTestConfig() *config.Config {
	return &config.Config{
		Domain: &config.Domain{
			Domains: map[string]*config.LabelSelector{
				"example.com": {},
				"another-example.com": {
					Selector: map[string]string{"app": "prod"},
				},
			},
		},
		GC: &gc.Config{
			StaleRevisionLastpinnedDebounce: time.Duration(1 * time.Minute),
		},
	}
}
