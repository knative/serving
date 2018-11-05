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

	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	netv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/gc"
	"github.com/knative/serving/pkg/reconciler"
	rtesting "github.com/knative/serving/pkg/reconciler/testing"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/config"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/resources"
	resourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/route/resources/names"
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
			simpleRunLatest("default", "first-reconcile", "not-ready", nil),
			simpleNotReadyConfig("default", "not-ready"),
			simpleNotReadyRevision("default",
				// Use the Revision name from the config.
				simpleNotReadyConfig("default", "not-ready").Status.LatestCreatedRevisionName,
			),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleRunLatest("default", "first-reconcile", "not-ready", &v1alpha1.RouteStatus{
				Conditions: duckv1alpha1.Conditions{{
					Type:    v1alpha1.RouteConditionAllTrafficAssigned,
					Status:  corev1.ConditionUnknown,
					Reason:  "RevisionMissing",
					Message: `Configuration "not-ready" is waiting for a Revision to become ready.`,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
					Status: corev1.ConditionUnknown,
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
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleRunLatest("default", "first-reconcile", "permanently-failed", &v1alpha1.RouteStatus{
				Conditions: duckv1alpha1.Conditions{{
					Type:    v1alpha1.RouteConditionAllTrafficAssigned,
					Status:  corev1.ConditionFalse,
					Reason:  "RevisionMissing",
					Message: `Configuration "permanently-failed" does not have any ready Revision.`,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
					Status: corev1.ConditionUnknown,
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
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleRunLatest("default", "first-reconcile", "not-ready", &v1alpha1.RouteStatus{
				Conditions: duckv1alpha1.Conditions{{
					Type:    v1alpha1.RouteConditionAllTrafficAssigned,
					Status:  corev1.ConditionUnknown,
					Reason:  "RevisionMissing",
					Message: `Configuration "not-ready" is waiting for a Revision to become ready.`,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
					Status: corev1.ConditionUnknown,
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
		Name: "simple route becomes ready, ingress unknown",
		Objects: []runtime.Object{
			simpleRunLatest("default", "becomes-ready", "config", nil),
			simpleReadyConfig("default", "config"),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
		},
		WantCreates: []metav1.Object{
			resources.MakeClusterIngress(
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
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleRunLatest("default", "becomes-ready", "config", &v1alpha1.RouteStatus{
				Domain:         "becomes-ready.default.example.com",
				DomainInternal: "becomes-ready.default.svc.cluster.local",
				Address:        &duckv1alpha1.Addressable{Hostname: "becomes-ready.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
					Status: corev1.ConditionUnknown,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionUnknown,
				}},
				Traffic: []v1alpha1.TrafficTarget{{
					RevisionName: "config-00001",
					Percent:      100,
				}},
			}),
		}},
		Key: "default/becomes-ready",
		// TODO(lichuqiang): config namespace validation in resource scope.
		SkipNamespaceValidation: true,
	}, {
		Name: "simple route becomes ready",
		Objects: []runtime.Object{
			simpleRunLatest("default", "becomes-ready", "config", nil),
			simpleReadyConfig("default", "config"),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
			simpleReadyIngress(
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
		},
		WantCreates: []metav1.Object{
			simpleK8sService(simpleRunLatest("default", "becomes-ready", "config", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleRunLatest("default", "becomes-ready", "config", &v1alpha1.RouteStatus{
				Domain:         "becomes-ready.default.example.com",
				DomainInternal: "becomes-ready.default.svc.cluster.local",
				Address:        &duckv1alpha1.Addressable{Hostname: "becomes-ready.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
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
			simpleReadyIngress(
				setDomain(simpleRunLatest("default", "create-svc-failure", "config", nil), "create-svc-failure.default.example.com"),
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
		},
		WantCreates: []metav1.Object{
			simpleK8sService(simpleRunLatest("default", "create-svc-failure", "config", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleRunLatest("default", "create-svc-failure", "config", &v1alpha1.RouteStatus{
				Domain:         "create-svc-failure.default.example.com",
				DomainInternal: "create-svc-failure.default.svc.cluster.local",
				Address:        &duckv1alpha1.Addressable{Hostname: "create-svc-failure.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
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
		Key: "default/create-svc-failure",
	}, {
		Name: "failure creating cluster ingress",
		Objects: []runtime.Object{
			simpleRunLatest("default", "ingress-create-failure", "config", nil),
			simpleReadyConfig("default", "config"),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
		},
		// We induce a failure creating the ClusterIngress.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "clusteringresses"),
		},
		WantCreates: []metav1.Object{
			// This is the Create we see for the cluter ingress, but we induce a failure.
			resources.MakeClusterIngress(
				setDomain(simpleRunLatest("default", "ingress-create-failure", "config", nil), "ingress-create-failure.default.example.com"),
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
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleRunLatest("default", "ingress-create-failure", "config", &v1alpha1.RouteStatus{
				Domain:         "ingress-create-failure.default.example.com",
				DomainInternal: "ingress-create-failure.default.svc.cluster.local",
				Address:        &duckv1alpha1.Addressable{Hostname: "ingress-create-failure.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
					Status: corev1.ConditionUnknown,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionUnknown,
				}},
				Traffic: []v1alpha1.TrafficTarget{{
					RevisionName: "config-00001",
					Percent:      100,
				}},
			}),
		}},
		Key: "default/ingress-create-failure",
		SkipNamespaceValidation: true,
	}, {
		Name: "steady state",
		Objects: []runtime.Object{
			simpleRunLatest("default", "steady-state", "config", &v1alpha1.RouteStatus{
				Domain:         "steady-state.default.example.com",
				DomainInternal: "steady-state.default.svc.cluster.local",
				Address:        &duckv1alpha1.Addressable{Hostname: "steady-state.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
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
			simpleReadyIngress(
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
			simpleK8sService(simpleRunLatest("default", "steady-state", "config", nil)),
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
					Address:        &duckv1alpha1.Addressable{Hostname: "different-domain.default.svc.cluster.local"},
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.RouteConditionAllTrafficAssigned,
						Status: corev1.ConditionTrue,
					}, {
						Type:   v1alpha1.RouteConditionIngressReady,
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
			simpleReadyIngress(
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
			simpleK8sService(simpleRunLatest("default", "different-domain", "config", nil)),
		},
		Key: "default/different-domain",
	}, {
		Name: "new latest created revision",
		Objects: []runtime.Object{
			simpleRunLatest("default", "new-latest-created", "config", &v1alpha1.RouteStatus{
				Domain:         "new-latest-created.default.example.com",
				DomainInternal: "new-latest-created.default.svc.cluster.local",
				Address:        &duckv1alpha1.Addressable{Hostname: "new-latest-created.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
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
			simpleReadyIngress(
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
			simpleK8sService(simpleRunLatest("default", "new-latest-created", "config", nil)),
		},
		// A new LatestCreatedRevisionName on the Configuration alone should result in no changes to the Route.
		Key: "default/new-latest-created",
	}, {
		Name: "new latest ready revision",
		Objects: []runtime.Object{
			simpleRunLatest("default", "new-latest-ready", "config", &v1alpha1.RouteStatus{
				Domain:         "new-latest-ready.default.example.com",
				DomainInternal: "new-latest-ready.default.svc.cluster.local",
				Address:        &duckv1alpha1.Addressable{Hostname: "new-latest-ready.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
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
			simpleReadyIngress(
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
			simpleK8sService(simpleRunLatest("default", "new-latest-ready", "config", nil)),
		},
		// A new LatestReadyRevisionName on the Configuration should result in the new Revision being rolled out.
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleReadyIngress(
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
				Address:        &duckv1alpha1.Addressable{Hostname: "new-latest-ready.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
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
		SkipNamespaceValidation: true,
	}, {
		Name: "failure updating cluster ingress",
		// Starting from the new latest ready, induce a failure updating the cluster ingress.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "clusteringresses"),
		},
		Objects: []runtime.Object{
			simpleRunLatest("default", "update-ci-failure", "config", &v1alpha1.RouteStatus{
				Domain:         "update-ci-failure.default.example.com",
				DomainInternal: "update-ci-failure.default.svc.cluster.local",
				Address:        &duckv1alpha1.Addressable{Hostname: "update-ci-failure.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
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
			setLatestReadyRevision(setLatestCreatedRevision(
				addConfigLabel(
					simpleReadyConfig("default", "config"),
					// The Route controller attaches our label to this Configuration.
					"serving.knative.dev/route", "update-ci-failure",
				),
				"config-00002",
			)),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
			// This is the name of the new revision we're referencing above.
			simpleReadyRevision("default", "config-00002"),
			simpleReadyIngress(
				setDomain(simpleRunLatest("default", "update-ci-failure", "config", nil), "update-ci-failure.default.example.com"),
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
			simpleK8sService(simpleRunLatest("default", "update-ci-failure", "config", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleReadyIngress(
				setDomain(simpleRunLatest("default", "update-ci-failure", "config", nil), "update-ci-failure.default.example.com"),
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
			Object: simpleRunLatest("default", "update-ci-failure", "config", &v1alpha1.RouteStatus{
				Domain:         "update-ci-failure.default.example.com",
				DomainInternal: "update-ci-failure.default.svc.cluster.local",
				Address:        &duckv1alpha1.Addressable{Hostname: "update-ci-failure.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
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
		Key: "default/update-ci-failure",
		SkipNamespaceValidation: true,
	}, {
		Name: "reconcile service mutation",
		Objects: []runtime.Object{
			simpleRunLatest("default", "svc-mutation", "config", &v1alpha1.RouteStatus{
				Domain:         "svc-mutation.default.example.com",
				DomainInternal: "svc-mutation.default.svc.cluster.local",
				Address:        &duckv1alpha1.Addressable{Hostname: "svc-mutation.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
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
			simpleReadyIngress(
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
			mutateService(simpleK8sService(simpleRunLatest("default", "svc-mutation", "config", nil))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleK8sService(simpleRunLatest("default", "svc-mutation", "config", nil)),
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
				Address:        &duckv1alpha1.Addressable{Hostname: "svc-mutation.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
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
			simpleReadyIngress(
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
			mutateService(simpleK8sService(simpleRunLatest("default", "svc-mutation", "config", nil))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleK8sService(simpleRunLatest("default", "svc-mutation", "config", nil)),
		}},
		Key: "default/svc-mutation",
	}, {
		// In #1789 we switched this to an ExternalName Service. Services created in
		// 0.1 will still have ClusterIP set, which is Forbidden for ExternalName
		// Services. Ensure that we drop the ClusterIP if it is set in the spec.
		Name: "drop cluster ip",
		Objects: []runtime.Object{
			simpleRunLatest("default", "cluster-ip", "config", &v1alpha1.RouteStatus{
				Domain:         "cluster-ip.default.example.com",
				DomainInternal: "cluster-ip.default.svc.cluster.local",
				Address:        &duckv1alpha1.Addressable{Hostname: "cluster-ip.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
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
			simpleReadyIngress(
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
			setClusterIP(simpleK8sService(simpleRunLatest("default", "cluster-ip", "config", nil)), "127.0.0.1"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleK8sService(simpleRunLatest("default", "cluster-ip", "config", nil)),
		}},
		Key: "default/cluster-ip",
	}, {
		// Make sure we fix the external name if something messes with it.
		Name: "fix external name",
		Objects: []runtime.Object{
			simpleRunLatest("default", "external-name", "config", &v1alpha1.RouteStatus{
				Domain:         "external-name.default.example.com",
				DomainInternal: "external-name.default.svc.cluster.local",
				Address:        &duckv1alpha1.Addressable{Hostname: "external-name.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
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
				"serving.knative.dev/route", "external-name",
			),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
			simpleReadyIngress(
				setDomain(simpleRunLatest("default", "external-name", "config", nil), "external-name.default.example.com"),
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
			setExternalName(simpleK8sService(simpleRunLatest("default", "external-name", "config", nil)), "this-is-the-wrong-name"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleK8sService(simpleRunLatest("default", "external-name", "config", nil)),
		}},
		Key: "default/external-name",
	}, {
		Name: "reconcile cluster ingress mutation",
		Objects: []runtime.Object{
			simpleRunLatest("default", "ingress-mutation", "config", &v1alpha1.RouteStatus{
				Domain:         "ingress-mutation.default.example.com",
				DomainInternal: "ingress-mutation.default.svc.cluster.local",
				Address:        &duckv1alpha1.Addressable{Hostname: "ingress-mutation.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
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
				"serving.knative.dev/route", "ingress-mutation",
			),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
			mutateIngress(simpleReadyIngress(
				setDomain(simpleRunLatest("default", "ingress-mutation", "config", nil), "ingress-mutation.default.example.com"),
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
			simpleK8sService(simpleRunLatest("default", "ingress-mutation", "config", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleReadyIngress(
				setDomain(simpleRunLatest("default", "ingress-mutation", "config", nil), "ingress-mutation.default.example.com"),
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
		Key: "default/ingress-mutation",
		SkipNamespaceValidation: true,
	}, {
		Name: "switch to a different config",
		Objects: []runtime.Object{
			// The status reflects "oldconfig", but the spec "newconfig".
			simpleRunLatest("default", "change-configs", "newconfig", &v1alpha1.RouteStatus{
				Domain:         "change-configs.default.example.com",
				DomainInternal: "change-configs.default.svc.cluster.local",
				Address:        &duckv1alpha1.Addressable{Hostname: "change-configs.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
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
			simpleReadyIngress(
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
			simpleK8sService(simpleRunLatest("default", "change-configs", "oldconfig", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// Updated to point to "newconfig" things.
			Object: simpleReadyIngress(
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
				Address:        &duckv1alpha1.Addressable{Hostname: "change-configs.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
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
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleRunLatest("default", "config-missing", "not-found", &v1alpha1.RouteStatus{
				Conditions: duckv1alpha1.Conditions{{
					Type:    v1alpha1.RouteConditionAllTrafficAssigned,
					Status:  corev1.ConditionFalse,
					Reason:  "ConfigurationMissing",
					Message: `Configuration "not-found" referenced in traffic not found.`,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
					Status: corev1.ConditionUnknown,
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
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simplePinned("default", "missing-revision-direct", "not-found", &v1alpha1.RouteStatus{
				Conditions: duckv1alpha1.Conditions{{
					Type:    v1alpha1.RouteConditionAllTrafficAssigned,
					Status:  corev1.ConditionFalse,
					Reason:  "RevisionMissing",
					Message: `Revision "not-found" referenced in traffic not found.`,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
					Status: corev1.ConditionUnknown,
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
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleRunLatest("default", "missing-revision-indirect", "config", &v1alpha1.RouteStatus{
				Conditions: duckv1alpha1.Conditions{{
					Type:    v1alpha1.RouteConditionAllTrafficAssigned,
					Status:  corev1.ConditionFalse,
					Reason:  "RevisionMissing",
					Message: `Revision "config-00001" referenced in traffic not found.`,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
					Status: corev1.ConditionUnknown,
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
			resources.MakeClusterIngress(
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
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simplePinned("default", "pinned-becomes-ready",
				// Use the config's revision name.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName, &v1alpha1.RouteStatus{
					Domain:         "pinned-becomes-ready.default.example.com",
					DomainInternal: "pinned-becomes-ready.default.svc.cluster.local",
					Address:        &duckv1alpha1.Addressable{Hostname: "pinned-becomes-ready.default.svc.cluster.local"},
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.RouteConditionAllTrafficAssigned,
						Status: corev1.ConditionTrue,
					}, {
						Type:   v1alpha1.RouteConditionIngressReady,
						Status: corev1.ConditionUnknown,
					}, {
						Type:   v1alpha1.RouteConditionReady,
						Status: corev1.ConditionUnknown,
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
		SkipNamespaceValidation: true,
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
			resources.MakeClusterIngress(
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
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: routeWithTraffic("default", "named-traffic-split", &v1alpha1.RouteStatus{
				Domain:         "named-traffic-split.default.example.com",
				DomainInternal: "named-traffic-split.default.svc.cluster.local",
				Address:        &duckv1alpha1.Addressable{Hostname: "named-traffic-split.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
					Status: corev1.ConditionUnknown,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionUnknown,
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
		SkipNamespaceValidation: true,
	}, {
		Name: "same revision targets",
		Objects: []runtime.Object{
			routeWithTraffic("default", "same-revision-targets", nil,
				v1alpha1.TrafficTarget{
					Name:              "gray",
					ConfigurationName: "gray",
					Percent:           50,
				}, v1alpha1.TrafficTarget{
					Name:         "also-gray",
					RevisionName: "gray-00001",
					Percent:      50,
				}),
			simpleReadyConfig("default", "gray"),
			addOwnerRef(
				simpleReadyRevision("default",
					// Use the Revision name from the config.
					simpleReadyConfig("default", "gray").Status.LatestReadyRevisionName,
				),
				or("Configuration", "gray"),
			),
		},
		WantCreates: []metav1.Object{
			resources.MakeClusterIngress(
				setDomain(routeWithTraffic("default", "same-revision-targets", nil,
					v1alpha1.TrafficTarget{
						Name:              "gray",
						ConfigurationName: "gray",
						Percent:           50,
					}, v1alpha1.TrafficTarget{
						Name:         "also-gray",
						RevisionName: "gray-00001",
						Percent:      50,
					}), "same-revision-targets.default.example.com"),
				&traffic.TrafficConfig{
					Targets: map[string][]traffic.RevisionTarget{
						"": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "gray").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
						"gray": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "gray").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
						"also-gray": {{
							TrafficTarget: v1alpha1.TrafficTarget{
								// Use the Revision name from the config.
								RevisionName: simpleReadyConfig("default", "gray").Status.LatestReadyRevisionName,
								Percent:      100,
							},
							Active: true,
						}},
					},
				},
			),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: routeWithTraffic("default", "same-revision-targets", &v1alpha1.RouteStatus{
				Domain:         "same-revision-targets.default.example.com",
				DomainInternal: "same-revision-targets.default.svc.cluster.local",
				Address:        &duckv1alpha1.Addressable{Hostname: "same-revision-targets.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
					Status: corev1.ConditionUnknown,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionUnknown,
				}},
				Traffic: []v1alpha1.TrafficTarget{{
					Name:         "gray",
					RevisionName: "gray-00001",
					Percent:      50,
				}, {
					Name:         "also-gray",
					RevisionName: "gray-00001",
					Percent:      50,
				}},
			}, v1alpha1.TrafficTarget{
				Name:              "gray",
				ConfigurationName: "gray",
				Percent:           50,
			}, v1alpha1.TrafficTarget{
				Name:         "also-gray",
				RevisionName: "gray-00001",
				Percent:      50,
			}),
		}},
		Key: "default/same-revision-targets",
		SkipNamespaceValidation: true,
	}, {
		Name: "change route configuration",
		// Start from a steady state referencing "blue", and modify the route spec to point to "green" instead.
		Objects: []runtime.Object{
			simpleRunLatest("default", "switch-configs", "green", &v1alpha1.RouteStatus{
				Domain:         "switch-configs.default.example.com",
				DomainInternal: "switch-configs.default.svc.cluster.local",
				Address:        &duckv1alpha1.Addressable{Hostname: "switch-configs.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
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
			simpleReadyIngress(
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
			simpleK8sService(simpleRunLatest("default", "switch-configs", "blue", nil)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: simpleReadyIngress(
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
				Address:        &duckv1alpha1.Addressable{Hostname: "switch-configs.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
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
		SkipNamespaceValidation: true,
	}, {
		Name: "Update stale lastPinned",
		Objects: []runtime.Object{
			simpleRunLatest("default", "stale-lastpinned", "config", &v1alpha1.RouteStatus{
				Domain:         "stale-lastpinned.default.example.com",
				DomainInternal: "stale-lastpinned.default.svc.cluster.local",
				Address:        &duckv1alpha1.Addressable{Hostname: "stale-lastpinned.default.svc.cluster.local"},
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionTrue,
				}, {
					Type:   v1alpha1.RouteConditionIngressReady,
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
				"serving.knative.dev/route", "stale-lastpinned",
			),
			setLastPinned(simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			), fakeCurTime.Add(-10*time.Minute)),
			simpleReadyIngress(
				setDomain(simpleRunLatest("default", "stale-lastpinned", "config", nil), "stale-lastpinned.default.example.com"),
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
			simpleK8sService(simpleRunLatest("default", "stale-lastpinned", "config", nil)),
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

func mutateIngress(ci *netv1alpha1.ClusterIngress) *netv1alpha1.ClusterIngress {
	// Thor's Hammer
	ci.Spec = netv1alpha1.IngressSpec{}
	return ci
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

func setExternalName(svc *corev1.Service, name string) *corev1.Service {
	svc.Spec.ExternalName = name
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

func simpleK8sService(r *v1alpha1.Route) *corev1.Service {
	// omit the error here, as we are sure the loadbalancer info is porvided.
	// return the service instance only, so that the result can be used in TableRow.
	svc, _ := resources.MakeK8sService(r, &netv1alpha1.ClusterIngress{Status: readyIngressStatus()})

	return svc
}

func simpleReadyIngress(r *v1alpha1.Route, tc *traffic.TrafficConfig) *netv1alpha1.ClusterIngress {
	return ingressWithStatus(r, tc, readyIngressStatus())
}

func readyIngressStatus() netv1alpha1.IngressStatus {
	status := netv1alpha1.IngressStatus{}
	status.InitializeConditions()
	status.MarkNetworkConfigured()
	status.MarkLoadBalancerReady([]netv1alpha1.LoadBalancerIngressStatus{
		{DomainInternal: resourcenames.K8sGatewayServiceFullname},
	})

	return status
}

func ingressWithStatus(r *v1alpha1.Route, tc *traffic.TrafficConfig, status netv1alpha1.IngressStatus) *netv1alpha1.ClusterIngress {
	ci := resources.MakeClusterIngress(r, tc)
	ci.Status = status

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

func setLastPinned(rev *v1alpha1.Revision, t time.Time) *v1alpha1.Revision {
	rev.Annotations["serving.knative.dev/lastPinned"] = v1alpha1.RevisionLastPinnedString(t)
	return rev
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

func revisionDefaultAnnotations() map[string]string {
	return map[string]string{
		"serving.knative.dev/lastPinned": v1alpha1.RevisionLastPinnedString(fakeCurTime.Add(-1 * time.Second)),
	}
}

func simpleReadyRevision(namespace, name string) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   namespace,
			Name:        name,
			Annotations: revisionDefaultAnnotations(),
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
			Namespace:   namespace,
			Name:        name,
			Annotations: revisionDefaultAnnotations(),
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
			Namespace:   namespace,
			Name:        name,
			Annotations: revisionDefaultAnnotations(),
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
		GC: &gc.Config{
			StaleRevisionLastpinnedDebounce: time.Duration(1 * time.Minute),
		},
	}
}
