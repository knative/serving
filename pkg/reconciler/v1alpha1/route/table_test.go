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

	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	istiov1alpha3 "github.com/knative/pkg/apis/istio/v1alpha3"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/config"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/resources"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/traffic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"

	rtesting "github.com/knative/serving/pkg/reconciler/testing"
	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
)

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
			simpleRunLatest("default", "first-reconcile", "not-ready", nil),
			simpleNotReadyConfig("default", "not-ready"),
			simpleNotReadyRevision("default",
				// Use the Revision name from the config.
				simpleNotReadyConfig("default", "not-ready").Status.LatestCreatedRevisionName,
			),
		},
		WantCreates: []metav1.Object{
			resources.MakeK8sService(simpleRunLatest("default", "first-reconcile", "not-ready", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleRunLatest("default", "first-reconcile", "not-ready", &v1alpha1.RouteStatus{
				Domain:         "first-reconcile.default.example.com",
				DomainInternal: "first-reconcile.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "first-reconcile.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:    v1alpha1.RouteConditionAllTrafficAssigned,
					Status:  corev1.ConditionUnknown,
					Reason:  "RevisionMissing",
					Message: `Configuration "not-ready" is waiting for a Revision to become ready.`,
				}, {
					Type:    v1alpha1.RouteConditionReady,
					Status:  corev1.ConditionUnknown,
					Reason:  "RevisionMissing",
					Message: `Configuration "not-ready" is waiting for a Revision to become ready.`,
				}},
			}),
		}},
		Key: "default/first-reconcile",
	}, {
		Name: "configuration permanently failed",
		Objects: []runtime.Object{
			simpleRunLatest("default", "first-reconcile", "permanently-failed", nil),
			simpleFailedConfig("default", "permanently-failed"),
			simpleFailedRevision("default",
				// Use the Revision name from the config.
				simpleFailedConfig("default", "permanently-failed").Status.LatestCreatedRevisionName,
			),
		},
		WantCreates: []metav1.Object{
			resources.MakeK8sService(simpleRunLatest("default", "first-reconcile", "permanently-failed", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleRunLatest("default", "first-reconcile", "permanently-failed", &v1alpha1.RouteStatus{
				Domain:         "first-reconcile.default.example.com",
				DomainInternal: "first-reconcile.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "first-reconcile.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:    v1alpha1.RouteConditionAllTrafficAssigned,
					Status:  corev1.ConditionFalse,
					Reason:  "RevisionMissing",
					Message: `Configuration "permanently-failed" does not have any ready Revision.`,
				}, {
					Type:    v1alpha1.RouteConditionReady,
					Status:  corev1.ConditionFalse,
					Reason:  "RevisionMissing",
					Message: `Configuration "permanently-failed" does not have any ready Revision.`,
				}},
			}),
		}},
		Key: "default/first-reconcile",
	}, {
		Name:    "failure updating route status",
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "routes"),
		},
		Objects: []runtime.Object{
			simpleRunLatest("default", "first-reconcile", "not-ready", nil),
			simpleNotReadyConfig("default", "not-ready"),
			simpleNotReadyRevision("default",
				// Use the Revision name from the config.
				simpleNotReadyConfig("default", "not-ready").Status.LatestCreatedRevisionName,
			),
		},
		WantCreates: []metav1.Object{
			resources.MakeK8sService(simpleRunLatest("default", "first-reconcile", "not-ready", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleRunLatest("default", "first-reconcile", "not-ready", &v1alpha1.RouteStatus{
				Domain:         "first-reconcile.default.example.com",
				DomainInternal: "first-reconcile.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "first-reconcile.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:    v1alpha1.RouteConditionAllTrafficAssigned,
					Status:  corev1.ConditionUnknown,
					Reason:  "RevisionMissing",
					Message: `Configuration "not-ready" is waiting for a Revision to become ready.`,
				}, {
					Type:    v1alpha1.RouteConditionReady,
					Status:  corev1.ConditionUnknown,
					Reason:  "RevisionMissing",
					Message: `Configuration "not-ready" is waiting for a Revision to become ready.`,
				}},
			}),
		}},
		Key: "default/first-reconcile",
	}, {
		Name: "simple route becomes ready",
		Objects: []runtime.Object{
			simpleRunLatest("default", "becomes-ready", "config", nil),
			simpleReadyConfig("default", "config"),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
		},
		WantCreates: []metav1.Object{
			resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "becomes-ready", "config", nil), "becomes-ready.default.example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			resources.MakeK8sService(simpleRunLatest("default", "becomes-ready", "config", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleRunLatest("default", "becomes-ready", "config", &v1alpha1.RouteStatus{
				Domain:         "becomes-ready.default.example.com",
				DomainInternal: "becomes-ready.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "becomes-ready.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionTrue,
				}},
				Traffic: []v1alpha1.TrafficTarget{{
					RevisionName: "config-00001",
					Percent:      100,
				}},
			}),
		}},
		Key: "default/becomes-ready",
	}, {
		Name: "failure creating k8s placeholder service",
		// We induce a failure creating the placeholder service
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "services"),
		},
		Objects: []runtime.Object{
			simpleRunLatest("default", "create-svc-failure", "config", nil),
			simpleReadyConfig("default", "config"),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
		},
		WantCreates: []metav1.Object{
			resources.MakeK8sService(simpleRunLatest("default", "create-svc-failure", "config", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleRunLatest("default", "create-svc-failure", "config", &v1alpha1.RouteStatus{
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionUnknown,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionUnknown,
				}},
			}),
		}},
		Key: "default/create-svc-failure",
	}, {
		Name: "failure creating virtual service",
		Objects: []runtime.Object{
			simpleRunLatest("default", "vs-create-failure", "config", nil),
			simpleReadyConfig("default", "config"),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
		},
		// We induce a failure creating the VirtualService.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "virtualservices"),
		},
		WantCreates: []metav1.Object{
			// This is the Create we see for the virtual service, but we induce a failure.
			resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "vs-create-failure", "config", nil), "vs-create-failure.default.example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			resources.MakeK8sService(simpleRunLatest("default", "vs-create-failure", "config", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleRunLatest("default", "vs-create-failure", "config", &v1alpha1.RouteStatus{
				Domain:         "vs-create-failure.default.example.com",
				DomainInternal: "vs-create-failure.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "vs-create-failure.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionUnknown,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionUnknown,
				}},
			}),
		}},
		Key: "default/vs-create-failure",
	}, {
		Name: "steady state",
		Objects: []runtime.Object{
			simpleRunLatest("default", "steady-state", "config", &v1alpha1.RouteStatus{
				Domain:         "steady-state.default.example.com",
				DomainInternal: "steady-state.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "steady-state.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionTrue,
				}},
				Traffic: []v1alpha1.TrafficTarget{{
					RevisionName: "config-00001",
					Percent:      100,
				}},
			}),
			addConfigLabel(
				simpleReadyConfig("default", "config"),
				// The Route controller attaches our label to this Configuration.
				"serving.knative.dev/route", "steady-state",
			),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
			resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "steady-state", "config", nil), "steady-state.default.example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			resources.MakeK8sService(simpleRunLatest("default", "steady-state", "config", nil)),
		},
		Key: "default/steady-state",
	}, {
		// This tests that when the Route is labelled differently, it is configured with a
		// different domain from config-domain.yaml.  This is otherwise a copy of the steady
		// state test above.
		Name: "different labels, different domain - steady state",
		Objects: []runtime.Object{
			addRouteLabel(
				simpleRunLatest("default", "different-domain", "config", &v1alpha1.RouteStatus{
					Domain:         "different-domain.default.another-example.com",
					DomainInternal: "different-domain.default.svc.cluster.local",
					Targetable:     &duckv1alpha1.Targetable{DomainInternal: "different-domain.default.svc.cluster.local"},
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.RouteConditionAllTrafficAssigned,
						Status: corev1.ConditionTrue,
					}, {
						Type:   v1alpha1.RouteConditionReady,
						Status: corev1.ConditionTrue,
					}},
					Traffic: []v1alpha1.TrafficTarget{{
						RevisionName: "config-00001",
						Percent:      100,
					}},
				}),
				"app", "prod",
			),
			addConfigLabel(
				simpleReadyConfig("default", "config"),
				// The Route controller attaches our label to this Configuration.
				"serving.knative.dev/route", "different-domain",
			),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
			resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "different-domain", "config", nil), "different-domain.default.another-example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			resources.MakeK8sService(simpleRunLatest("default", "different-domain", "config", nil)),
		},
		Key: "default/different-domain",
	}, {
		Name: "new latest created revision",
		Objects: []runtime.Object{
			simpleRunLatest("default", "new-latest-created", "config", &v1alpha1.RouteStatus{
				Domain:         "new-latest-created.default.example.com",
				DomainInternal: "new-latest-created.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "new-latest-created.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionTrue,
				}},
				Traffic: []v1alpha1.TrafficTarget{{
					RevisionName: "config-00001",
					Percent:      100,
				}},
			}),
			setLatestCreatedRevision(
				addConfigLabel(
					simpleReadyConfig("default", "config"),
					// The Route controller attaches our label to this Configuration.
					"serving.knative.dev/route", "new-latest-created",
				),
				"config-00002",
			),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
			// This is the name of the new revision we're referencing above.
			simpleNotReadyRevision("default", "config-00002"),
			resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "new-latest-created", "config", nil), "new-latest-created.default.example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			resources.MakeK8sService(simpleRunLatest("default", "new-latest-created", "config", nil)),
		},
		// A new LatestCreatedRevisionName on the Configuration alone should result in no changes to the Route.
		Key: "default/new-latest-created",
	}, {
		Name: "new latest ready revision",
		Objects: []runtime.Object{
			simpleRunLatest("default", "new-latest-ready", "config", &v1alpha1.RouteStatus{
				Domain:         "new-latest-ready.default.example.com",
				DomainInternal: "new-latest-ready.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "new-latest-ready.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionTrue,
				}},
				Traffic: []v1alpha1.TrafficTarget{{
					ConfigurationName: "config",
					RevisionName:      "config-00001",
					Percent:           100,
				}},
			}),
			setLatestReadyRevision(setLatestCreatedRevision(
				addConfigLabel(
					simpleReadyConfig("default", "config"),
					// The Route controller attaches our label to this Configuration.
					"serving.knative.dev/route", "new-latest-ready",
				),
				"config-00002",
			)),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
			// This is the name of the new revision we're referencing above.
			simpleReadyRevision("default", "config-00002"),
			resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "new-latest-ready", "config", nil), "new-latest-ready.default.example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			resources.MakeK8sService(simpleRunLatest("default", "new-latest-ready", "config", nil)),
		},
		// A new LatestReadyRevisionName on the Configuration should result in the new Revision being rolled out.
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "new-latest-ready", "config", nil), "new-latest-ready.default.example.com"),
				&traffic.TrafficConfig{
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
		}, {
			Object: simpleRunLatest("default", "new-latest-ready", "config", &v1alpha1.RouteStatus{
				Domain:         "new-latest-ready.default.example.com",
				DomainInternal: "new-latest-ready.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "new-latest-ready.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionTrue,
				}},
				Traffic: []v1alpha1.TrafficTarget{{
					RevisionName: "config-00002",
					Percent:      100,
				}},
			}),
		}},
		Key: "default/new-latest-ready",
	}, {
		Name: "failure updating virtual service",
		// Starting from the new latest ready, induce a failure updating the virtual service.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "virtualservices"),
		},
		Objects: []runtime.Object{
			simpleRunLatest("default", "update-vs-failure", "config", &v1alpha1.RouteStatus{
				Domain:         "update-vs-failure.default.example.com",
				DomainInternal: "update-vs-failure.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "update-vs-failure.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionTrue,
				}},
				Traffic: []v1alpha1.TrafficTarget{{
					ConfigurationName: "config",
					RevisionName:      "config-00001",
					Percent:           100,
				}},
			}),
			setLatestReadyRevision(setLatestCreatedRevision(
				addConfigLabel(
					simpleReadyConfig("default", "config"),
					// The Route controller attaches our label to this Configuration.
					"serving.knative.dev/route", "update-vs-failure",
				),
				"config-00002",
			)),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
			// This is the name of the new revision we're referencing above.
			simpleReadyRevision("default", "config-00002"),
			resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "update-vs-failure", "config", nil), "update-vs-failure.default.example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			resources.MakeK8sService(simpleRunLatest("default", "update-vs-failure", "config", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "update-vs-failure", "config", nil), "update-vs-failure.default.example.com"),
				&traffic.TrafficConfig{
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
		Key: "default/update-vs-failure",
	}, {
		Name: "reconcile service mutation",
		Objects: []runtime.Object{
			simpleRunLatest("default", "svc-mutation", "config", &v1alpha1.RouteStatus{
				Domain:         "svc-mutation.default.example.com",
				DomainInternal: "svc-mutation.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "svc-mutation.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionTrue,
				}},
				Traffic: []v1alpha1.TrafficTarget{{
					RevisionName: "config-00001",
					Percent:      100,
				}},
			}),
			addConfigLabel(
				simpleReadyConfig("default", "config"),
				// The Route controller attaches our label to this Configuration.
				"serving.knative.dev/route", "svc-mutation",
			),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
			resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "svc-mutation", "config", nil), "svc-mutation.default.example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			mutateService(resources.MakeK8sService(simpleRunLatest("default", "svc-mutation", "config", nil))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: resources.MakeK8sService(simpleRunLatest("default", "svc-mutation", "config", nil)),
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
			simpleRunLatest("default", "svc-mutation", "config", &v1alpha1.RouteStatus{
				Domain:         "svc-mutation.default.example.com",
				DomainInternal: "svc-mutation.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "svc-mutation.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionTrue,
				}},
				Traffic: []v1alpha1.TrafficTarget{{
					ConfigurationName: "config",
					RevisionName:      "config-00001",
					Percent:           100,
				}},
			}),
			addConfigLabel(
				simpleReadyConfig("default", "config"),
				// The Route controller attaches our label to this Configuration.
				"serving.knative.dev/route", "svc-mutation",
			),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
			resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "svc-mutation", "config", nil), "svc-mutation.default.example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			mutateService(resources.MakeK8sService(simpleRunLatest("default", "svc-mutation", "config", nil))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: resources.MakeK8sService(simpleRunLatest("default", "svc-mutation", "config", nil)),
		}},
		Key: "default/svc-mutation",
	}, {
		Name: "allow cluster ip",
		Objects: []runtime.Object{
			simpleRunLatest("default", "cluster-ip", "config", &v1alpha1.RouteStatus{
				Domain:         "cluster-ip.default.example.com",
				DomainInternal: "cluster-ip.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "cluster-ip.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionTrue,
				}},
				Traffic: []v1alpha1.TrafficTarget{{
					RevisionName: "config-00001",
					Percent:      100,
				}},
			}),
			addConfigLabel(
				simpleReadyConfig("default", "config"),
				// The Route controller attaches our label to this Configuration.
				"serving.knative.dev/route", "cluster-ip",
			),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
			resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "cluster-ip", "config", nil), "cluster-ip.default.example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			setClusterIP(resources.MakeK8sService(simpleRunLatest("default", "cluster-ip", "config", nil)), "127.0.0.1"),
		},
		Key: "default/cluster-ip",
	}, {
		Name: "reconcile virtual service mutation",
		Objects: []runtime.Object{
			simpleRunLatest("default", "virt-svc-mutation", "config", &v1alpha1.RouteStatus{
				Domain:         "virt-svc-mutation.default.example.com",
				DomainInternal: "virt-svc-mutation.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "virt-svc-mutation.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionTrue,
				}},
				Traffic: []v1alpha1.TrafficTarget{{
					RevisionName: "config-00001",
					Percent:      100,
				}},
			}),
			addConfigLabel(
				simpleReadyConfig("default", "config"),
				// The Route controller attaches our label to this Configuration.
				"serving.knative.dev/route", "virt-svc-mutation",
			),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
			mutateVirtualService(resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "virt-svc-mutation", "config", nil), "virt-svc-mutation.default.example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			)),
			resources.MakeK8sService(simpleRunLatest("default", "virt-svc-mutation", "config", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "virt-svc-mutation", "config", nil), "virt-svc-mutation.default.example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
		}},
		Key: "default/virt-svc-mutation",
	}, {
		Name: "switch to a different config",
		Objects: []runtime.Object{
			// The status reflects "oldconfig", but the spec "newconfig".
			simpleRunLatest("default", "change-configs", "newconfig", &v1alpha1.RouteStatus{
				Domain:         "change-configs.default.example.com",
				DomainInternal: "change-configs.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "change-configs.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionTrue,
				}},
				Traffic: []v1alpha1.TrafficTarget{{
					ConfigurationName: "oldconfig",
					RevisionName:      "oldconfig-00001",
					Percent:           100,
				}},
			}),
			// Both configs exist, but only "oldconfig" is labelled.
			addConfigLabel(
				simpleReadyConfig("default", "oldconfig"),
				// The Route controller attaches our label to this Configuration.
				"serving.knative.dev/route", "change-configs",
			),
			simpleReadyConfig("default", "newconfig"),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "oldconfig").Status.LatestReadyRevisionName,
			),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "newconfig").Status.LatestReadyRevisionName,
			),
			resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "change-configs", "oldconfig", nil), "change-configs.default.example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "oldconfig").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			resources.MakeK8sService(simpleRunLatest("default", "change-configs", "oldconfig", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// Updated to point to "newconfig" things.
			Object: resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "change-configs", "newconfig", nil), "change-configs.default.example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "newconfig").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
		}, {
			// Status updated to "newconfig"
			Object: simpleRunLatest("default", "change-configs", "newconfig", &v1alpha1.RouteStatus{
				Domain:         "change-configs.default.example.com",
				DomainInternal: "change-configs.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "change-configs.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionTrue,
				}},
				Traffic: []v1alpha1.TrafficTarget{{
					RevisionName: "newconfig-00001",
					Percent:      100,
				}},
			}),
		}},
		Key: "default/change-configs",
	}, {
		Name: "configuration missing",
		Objects: []runtime.Object{
			simpleRunLatest("default", "config-missing", "not-found", nil),
		},
		WantCreates: []metav1.Object{
			resources.MakeK8sService(simpleRunLatest("default", "config-missing", "not-found", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleRunLatest("default", "config-missing", "not-found", &v1alpha1.RouteStatus{
				Domain:         "config-missing.default.example.com",
				DomainInternal: "config-missing.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "config-missing.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:    v1alpha1.RouteConditionAllTrafficAssigned,
					Status:  corev1.ConditionFalse,
					Reason:  "ConfigurationMissing",
					Message: `Configuration "not-found" referenced in traffic not found.`,
				}, {
					Type:    v1alpha1.RouteConditionReady,
					Status:  corev1.ConditionFalse,
					Reason:  "ConfigurationMissing",
					Message: `Configuration "not-found" referenced in traffic not found.`,
				}},
			}),
		}},
		Key: "default/config-missing",
	}, {
		Name: "revision missing (direct)",
		Objects: []runtime.Object{
			simplePinned("default", "missing-revision-direct", "not-found", nil),
			simpleReadyConfig("default", "config"),
		},
		WantCreates: []metav1.Object{
			resources.MakeK8sService(simpleRunLatest("default", "missing-revision-direct", "config", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simplePinned("default", "missing-revision-direct", "not-found", &v1alpha1.RouteStatus{
				Domain:         "missing-revision-direct.default.example.com",
				DomainInternal: "missing-revision-direct.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "missing-revision-direct.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:    v1alpha1.RouteConditionAllTrafficAssigned,
					Status:  corev1.ConditionFalse,
					Reason:  "RevisionMissing",
					Message: `Revision "not-found" referenced in traffic not found.`,
				}, {
					Type:    v1alpha1.RouteConditionReady,
					Status:  corev1.ConditionFalse,
					Reason:  "RevisionMissing",
					Message: `Revision "not-found" referenced in traffic not found.`,
				}},
			}),
		}},
		Key: "default/missing-revision-direct",
	}, {
		Name: "revision missing (indirect)",
		Objects: []runtime.Object{
			simpleRunLatest("default", "missing-revision-indirect", "config", nil),
			simpleReadyConfig("default", "config"),
		},
		WantCreates: []metav1.Object{
			resources.MakeK8sService(simpleRunLatest("default", "missing-revision-indirect", "config", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleRunLatest("default", "missing-revision-indirect", "config", &v1alpha1.RouteStatus{
				Domain:         "missing-revision-indirect.default.example.com",
				DomainInternal: "missing-revision-indirect.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "missing-revision-indirect.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:    v1alpha1.RouteConditionAllTrafficAssigned,
					Status:  corev1.ConditionFalse,
					Reason:  "RevisionMissing",
					Message: `Revision "config-00001" referenced in traffic not found.`,
				}, {
					Type:    v1alpha1.RouteConditionReady,
					Status:  corev1.ConditionFalse,
					Reason:  "RevisionMissing",
					Message: `Revision "config-00001" referenced in traffic not found.`,
				}},
			}),
		}},
		Key: "default/missing-revision-indirect",
	}, {
		Name: "pinned route becomes ready",
		Objects: []runtime.Object{
			simplePinned("default", "pinned-becomes-ready",
				// Use the Revision name from the config
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName, nil),
			simpleReadyConfig("default", "config"),
			addOwnerRef(
				simpleReadyRevision("default",
					// Use the Revision name from the config.
					simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
				),
				or("Configuration", "config"),
			),
		},
		WantCreates: []metav1.Object{
			resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "pinned-becomes-ready", "config", nil), "pinned-becomes-ready.default.example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			resources.MakeK8sService(simpleRunLatest("default", "pinned-becomes-ready", "config", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simplePinned("default", "pinned-becomes-ready",
				// Use the config's revision name.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName, &v1alpha1.RouteStatus{
					Domain:         "pinned-becomes-ready.default.example.com",
					DomainInternal: "pinned-becomes-ready.default.svc.cluster.local",
					Targetable:     &duckv1alpha1.Targetable{DomainInternal: "pinned-becomes-ready.default.svc.cluster.local"},
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.RouteConditionAllTrafficAssigned,
						Status: corev1.ConditionTrue,
					}, {
						Type:   v1alpha1.RouteConditionReady,
						Status: corev1.ConditionTrue,
					}},
					Traffic: []v1alpha1.TrafficTarget{{
						// TODO(#1495): This is established thru labels instead of OwnerReferences.
						// ConfigurationName: "config",
						RevisionName: "config-00001",
						Percent:      100,
					}},
				}),
		}},
		Key: "default/pinned-becomes-ready",
	}, {
		Name: "traffic split becomes ready",
		Objects: []runtime.Object{
			routeWithTraffic("default", "named-traffic-split", nil,
				v1alpha1.TrafficTarget{
					ConfigurationName: "blue",
					Percent:           50,
				}, v1alpha1.TrafficTarget{
					ConfigurationName: "green",
					Percent:           50,
				}),
			simpleReadyConfig("default", "blue"),
			simpleReadyConfig("default", "green"),
			addOwnerRef(
				simpleReadyRevision("default",
					// Use the Revision name from the config.
					simpleReadyConfig("default", "blue").Status.LatestReadyRevisionName,
				),
				or("Configuration", "blue"),
			),
			addOwnerRef(
				simpleReadyRevision("default",
					// Use the Revision name from the config.
					simpleReadyConfig("default", "green").Status.LatestReadyRevisionName,
				),
				or("Configuration", "green"),
			),
		},
		WantCreates: []metav1.Object{
			resources.MakeVirtualService(
				setDomain(routeWithTraffic("default", "named-traffic-split", nil,
					v1alpha1.TrafficTarget{
						ConfigurationName: "blue",
						Percent:           50,
					}, v1alpha1.TrafficTarget{
						ConfigurationName: "green",
						Percent:           50,
					}), "named-traffic-split.default.example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "blue").Status.LatestReadyRevisionName,
								Percent:      50,
							},
							Active: true,
						}, {
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "green").Status.LatestReadyRevisionName,
								Percent:      50,
							},
							Active: true,
						}},
					},
				},
			),
			resources.MakeK8sService(routeWithTraffic("default", "named-traffic-split", nil,
				v1alpha1.TrafficTarget{
					ConfigurationName: "blue",
					Percent:           50,
				}, v1alpha1.TrafficTarget{
					ConfigurationName: "green",
					Percent:           50,
				})),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: routeWithTraffic("default", "named-traffic-split", &v1alpha1.RouteStatus{
				Domain:         "named-traffic-split.default.example.com",
				DomainInternal: "named-traffic-split.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "named-traffic-split.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionTrue,
				}},
				Traffic: []v1alpha1.TrafficTarget{{
					RevisionName: "blue-00001",
					Percent:      50,
				}, {
					RevisionName: "green-00001",
					Percent:      50,
				}},
			}, v1alpha1.TrafficTarget{
				ConfigurationName: "blue",
				Percent:           50,
			}, v1alpha1.TrafficTarget{
				ConfigurationName: "green",
				Percent:           50,
			}),
		}},
		Key: "default/named-traffic-split",
	}, {
		Name: "change route configuration",
		// Start from a steady state referencing "blue", and modify the route spec to point to "green" instead.
		Objects: []runtime.Object{
			simpleRunLatest("default", "switch-configs", "green", &v1alpha1.RouteStatus{
				Domain:         "switch-configs.default.example.com",
				DomainInternal: "switch-configs.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "switch-configs.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionTrue,
				}},
				Traffic: []v1alpha1.TrafficTarget{{
					ConfigurationName: "blue",
					RevisionName:      "blue-00001",
					Percent:           100,
				}},
			}),
			addConfigLabel(
				simpleReadyConfig("default", "blue"),
				// The Route controller attaches our label to this Configuration.
				"serving.knative.dev/route", "switch-configs",
			),
			simpleReadyConfig("default", "green"),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "blue").Status.LatestReadyRevisionName,
			),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "green").Status.LatestReadyRevisionName,
			),
			resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "switch-configs", "blue", nil), "switch-configs.default.example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "blue").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
			resources.MakeK8sService(simpleRunLatest("default", "switch-configs", "blue", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: resources.MakeVirtualService(
				setDomain(simpleRunLatest("default", "switch-configs", "green", nil), "switch-configs.default.example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "green").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
		}, {
			Object: simpleRunLatest("default", "switch-configs", "green", &v1alpha1.RouteStatus{
				Domain:         "switch-configs.default.example.com",
				DomainInternal: "switch-configs.default.svc.cluster.local",
				Targetable:     &duckv1alpha1.Targetable{DomainInternal: "switch-configs.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionTrue,
				}},
				Traffic: []v1alpha1.TrafficTarget{{
					RevisionName: "green-00001",
					Percent:      100,
				}},
			}),
		}},
		Key: "default/switch-configs",
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
			virtualServiceLister: listers.GetVirtualServiceLister(),
			tracker:              &rtesting.NullTracker{},
			configStore: &testConfigStore{
				config: ReconcilerTestConfig(),
			},
		}
	}))
}

func mutateVirtualService(vs *istiov1alpha3.VirtualService) *istiov1alpha3.VirtualService {
	// Thor's Hammer
	vs.Spec = istiov1alpha3.VirtualServiceSpec{}
	return vs
}

func mutateService(svc *corev1.Service) *corev1.Service {
	// Thor's Hammer
	svc.Spec = corev1.ServiceSpec{}
	return svc
}

func setClusterIP(svc *corev1.Service, ip string) *corev1.Service {
	svc.Spec.ClusterIP = ip
	return svc
}

func routeWithTraffic(namespace, name string, status *v1alpha1.RouteStatus, traffic ...v1alpha1.TrafficTarget) *v1alpha1.Route {
	route := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: v1alpha1.RouteSpec{
			Traffic: traffic,
		},
	}
	if status != nil {
		route.Status = *status
	}
	return route
}

func simplePinned(namespace, name, revision string, status *v1alpha1.RouteStatus) *v1alpha1.Route {
	return routeWithTraffic(namespace, name, status, v1alpha1.TrafficTarget{
		RevisionName: revision,
		Percent:      100,
	})
}

func simpleRunLatest(namespace, name, config string, status *v1alpha1.RouteStatus) *v1alpha1.Route {
	return routeWithTraffic(namespace, name, status, v1alpha1.TrafficTarget{
		ConfigurationName: config,
		Percent:           100,
	})
}

func setDomain(route *v1alpha1.Route, domain string) *v1alpha1.Route {
	route.Status.Domain = domain
	return route
}

func simpleNotReadyConfig(namespace, name string) *v1alpha1.Configuration {
	cfg := &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            name,
			ResourceVersion: "v1",
		},
		Spec: v1alpha1.ConfigurationSpec{
			Generation: 1,
			RevisionTemplate: v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					Container: corev1.Container{
						Image: "busybox",
					},
				},
			},
		},
	}
	cfg.Status.InitializeConditions()
	cfg.Status.SetLatestCreatedRevisionName(name + "-00001")
	return cfg
}

func simpleFailedConfig(namespace, name string) *v1alpha1.Configuration {
	cfg := &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            name,
			ResourceVersion: "v1",
		},
		Spec: v1alpha1.ConfigurationSpec{
			Generation: 1,
			RevisionTemplate: v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					Container: corev1.Container{
						Image: "busybox",
					},
				},
			},
		},
	}
	cfg.Status.InitializeConditions()
	cfg.Status.MarkLatestCreatedFailed(name+"-00001", "should have used ko")
	return cfg
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

func simpleReadyConfig(namespace, name string) *v1alpha1.Configuration {
	return setLatestReadyRevision(simpleNotReadyConfig(namespace, name))
}

func setLatestCreatedRevision(cfg *v1alpha1.Configuration, name string) *v1alpha1.Configuration {
	cfg.Status.SetLatestCreatedRevisionName(name)
	return cfg
}

func setLatestReadyRevision(cfg *v1alpha1.Configuration) *v1alpha1.Configuration {
	cfg.Status.SetLatestReadyRevisionName(cfg.Status.LatestCreatedRevisionName)
	return cfg
}

func addRouteLabel(route *v1alpha1.Route, key, value string) *v1alpha1.Route {
	if route.Labels == nil {
		route.Labels = make(map[string]string)
	}
	route.Labels[key] = value
	return route
}

func addConfigLabel(config *v1alpha1.Configuration, key, value string) *v1alpha1.Configuration {
	if config.Labels == nil {
		config.Labels = make(map[string]string)
	}
	config.Labels[key] = value
	return config
}

func simpleReadyRevision(namespace, name string) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: v1alpha1.RevisionStatus{
			Conditions: duckv1alpha1.Conditions{{
				Type:   v1alpha1.RevisionConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
}

func simpleNotReadyRevision(namespace, name string) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: v1alpha1.RevisionStatus{
			Conditions: duckv1alpha1.Conditions{{
				Type:   v1alpha1.RevisionConditionReady,
				Status: corev1.ConditionUnknown,
			}},
		},
	}
}

func simpleFailedRevision(namespace, name string) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: v1alpha1.RevisionStatus{
			Conditions: duckv1alpha1.Conditions{{
				Type:   v1alpha1.RevisionConditionReady,
				Status: corev1.ConditionFalse,
			}},
		},
	}
}

func addOwnerRef(rev *v1alpha1.Revision, o []metav1.OwnerReference) *v1alpha1.Revision {
	rev.OwnerReferences = o
	return rev
}

// or builds OwnerReferences for a child of a Service
func or(kind, name string) []metav1.OwnerReference {
	boolTrue := true
	return []metav1.OwnerReference{{
		APIVersion:         v1alpha1.SchemeGroupVersion.String(),
		Kind:               kind,
		Name:               name,
		Controller:         &boolTrue,
		BlockOwnerDeletion: &boolTrue,
	}}
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
	}
}
