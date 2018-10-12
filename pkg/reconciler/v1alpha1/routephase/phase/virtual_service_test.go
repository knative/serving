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

package phase

import (
	"fmt"
	"testing"

	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	istiov1alpha3 "github.com/knative/pkg/apis/istio/v1alpha3"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/resources"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/traffic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"

	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
)

func TestVirtualServiceReconcile(t *testing.T) {
	scenarios := PhaseTests{{
		// When the configuration is not ready there should be
		Name:     "configuration not yet ready",
		Context:  contextWithDefaultDomain("example.com"),
		Resource: simpleRunLatest("default", "first-reconcile", "not-ready"),
		Objects: Objects{
			simpleNotReadyConfig("default", "not-ready"),
			simpleNotReadyRevision("default",
				// Use the Revision name from the config.
				simpleNotReadyConfig("default", "not-ready").Status.LatestCreatedRevisionName,
			),
		},
		ExpectedStatus: v1alpha1.RouteStatus{
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
		},
	}, {
		Name:     "configuration permanently failed",
		Context:  contextWithDefaultDomain("example.com"),
		Resource: simpleRunLatest("default", "first-reconcile", "permanently-failed"),
		Objects: []runtime.Object{
			simpleFailedConfig("default", "permanently-failed"),
			simpleFailedRevision("default",
				// Use the Revision name from the config.
				simpleFailedConfig("default", "permanently-failed").Status.LatestCreatedRevisionName,
			),
		},
		ExpectedStatus: v1alpha1.RouteStatus{
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
		},
	}, {
		Name:     "simple route becomes ready",
		Context:  contextWithDefaultDomain("example.com"),
		Resource: simpleRunLatest("default", "becomes-ready", "config"),
		Objects: Objects{
			simpleReadyConfig("default", "config"),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
		},
		ExpectedCreates: Creates{
			resources.MakeVirtualService2(
				"becomes-ready.default.example.com",
				simpleRunLatest("default", "becomes-ready", "config"),
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
		ExpectedStatus: v1alpha1.RouteStatus{
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
		},
	}, {
		Name:     "failure creating virtual service",
		Context:  contextWithDefaultDomain("example.com"),
		Resource: simpleRunLatest("default", "vs-create-failure", "config"),
		Objects: Objects{
			simpleReadyConfig("default", "config"),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
		},
		// We induce a failure creating the VirtualService.
		Failures: Failures{
			InduceFailure("create", "virtualservices"),
		},
		ExpectError:    true,
		ExpectedStatus: v1alpha1.RouteStatus{},
		ExpectedCreates: Creates{
			// This is the Create we see for the virtual service, but we induce a failure.
			resources.MakeVirtualService2(
				"vs-create-failure.default.example.com",
				simpleRunLatest("default", "vs-create-failure", "config"),
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
	}, {
		Name:     "steady state",
		Context:  contextWithDefaultDomain("example.com"),
		Resource: simpleRunLatest("default", "steady-state", "config"),
		Objects: Objects{
			simpleReadyConfig("default", "config"),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
			resources.MakeVirtualService2(
				"steady-state.default.example.com",
				simpleRunLatest("default", "steady-state", "config"),
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
		ExpectedStatus: v1alpha1.RouteStatus{
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
		},
	}, {
		Name:     "different domain",
		Context:  contextWithDefaultDomain("another-example.com"),
		Resource: simpleRunLatest("default", "different-domain", "config"),
		Objects: Objects{
			simpleReadyConfig("default", "config"),
			simpleReadyRevision("default",
				// Use the Revision name from the config.
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
			resources.MakeVirtualService2(
				"different-domain.default.example.com",
				simpleRunLatest("default", "different-domain", "config"),
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
		ExpectedUpdates: Updates{
			resources.MakeVirtualService2(
				"different-domain.default.another-example.com",
				simpleRunLatest("default", "different-domain", "config"),
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
		ExpectedStatus: v1alpha1.RouteStatus{
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
		},
	},
		{
			// A new LatestCreatedRevisionName on the Configuration alone should result in no changes to the Route.
			Name:     "new latest created revision",
			Context:  contextWithDefaultDomain("example.com"),
			Resource: simpleRunLatest("default", "new-latest-created", "config"),
			Objects: Objects{
				setLatestCreatedRevision(
					simpleReadyConfig("default", "config"),
					"config-00002",
				),
				simpleReadyRevision("default",
					// Use the Revision name from the config.
					simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
				),
				// This is the name of the new revision we're referencing above.
				simpleNotReadyRevision("default", "config-00002"),
				resources.MakeVirtualService2(
					"new-latest-created.default.example.com",
					simpleRunLatest("default", "new-latest-created", "config"),
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
			ExpectedStatus: v1alpha1.RouteStatus{
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
			},
		}, {
			Name:     "new latest ready revision",
			Context:  contextWithDefaultDomain("example.com"),
			Resource: simpleRunLatest("default", "new-latest-ready", "config"),
			Objects: Objects{
				setLatestReadyRevision(setLatestCreatedRevision(
					simpleReadyConfig("default", "config"),
					"config-00002",
				)),
				simpleReadyRevision("default",
					// Use the Revision name from the config.
					simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
				),
				// This is the name of the new revision we're referencing above.
				simpleReadyRevision("default", "config-00002"),
				resources.MakeVirtualService2(
					"new-latest-ready.default.example.com",
					simpleRunLatest("default", "new-latest-ready", "config"),
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
			// A new LatestReadyRevisionName on the Configuration should result in the new Revision being rolled out.
			ExpectedUpdates: Updates{
				resources.MakeVirtualService2(
					"new-latest-ready.default.example.com",
					simpleRunLatest("default", "new-latest-ready", "config"),
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
			},
			ExpectedStatus: v1alpha1.RouteStatus{
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
			},
		}, {
			Name: "failure updating virtual service",
			// Starting from the new latest ready, induce a failure updating the virtual service.
			ExpectError: true,
			Failures: Failures{
				InduceFailure("update", "virtualservices"),
			},
			Context:  contextWithDefaultDomain("example.com"),
			Resource: simpleRunLatest("default", "update-vs-failure", "config"),
			Objects: Objects{
				setLatestReadyRevision(setLatestCreatedRevision(
					simpleReadyConfig("default", "config"),
					"config-00002",
				)),
				simpleReadyRevision("default",
					// Use the Revision name from the config.
					simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
				),
				// This is the name of the new revision we're referencing above.
				simpleReadyRevision("default", "config-00002"),
				resources.MakeVirtualService2(
					"update-vs-failure.default.example.com",
					simpleRunLatest("default", "update-vs-failure", "config"),
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
				resources.MakeK8sService(simpleRunLatest("default", "update-vs-failure", "config")),
			},
			ExpectedUpdates: Updates{
				resources.MakeVirtualService2(
					"update-vs-failure.default.example.com",
					simpleRunLatest("default", "update-vs-failure", "config"),
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
			},
			ExpectedStatus: v1alpha1.RouteStatus{},
		}, {
			Name:     "reconcile virtual service mutation",
			Context:  contextWithDefaultDomain("example.com"),
			Resource: simpleRunLatest("default", "virt-svc-mutation", "config"),
			Objects: Objects{
				simpleReadyConfig("default", "config"),
				simpleReadyRevision("default",
					// Use the Revision name from the config.
					simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
				),
				mutateVirtualService(resources.MakeVirtualService2(
					"virt-svc-mutation.default.example.com",
					simpleRunLatest("default", "virt-svc-mutation", "config"),
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
			},
			ExpectedUpdates: Updates{
				resources.MakeVirtualService2(
					"virt-svc-mutation.default.example.com",
					simpleRunLatest("default", "virt-svc-mutation", "config"),
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
			ExpectedStatus: v1alpha1.RouteStatus{
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
			},
		}, {
			Name: "switch to a different config",
			// The status reflects "oldconfig", but the spec "newconfig".
			Context:  contextWithDefaultDomain("example.com"),
			Resource: simpleRunLatest("default", "change-configs", "newconfig"),
			Objects: Objects{
				// Both configs exist, but only "oldconfig" is labelled.
				simpleReadyConfig("default", "oldconfig"),
				simpleReadyConfig("default", "newconfig"),
				simpleReadyRevision("default",
					// Use the Revision name from the config.
					simpleReadyConfig("default", "oldconfig").Status.LatestReadyRevisionName,
				),
				simpleReadyRevision("default",
					// Use the Revision name from the config.
					simpleReadyConfig("default", "newconfig").Status.LatestReadyRevisionName,
				),
				resources.MakeVirtualService2(
					"change-configs.default.example.com",
					simpleRunLatest("default", "change-configs", "oldconfig"),
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
				resources.MakeK8sService(simpleRunLatest("default", "change-configs", "oldconfig")),
			},
			ExpectedUpdates: Updates{
				// Updated to point to "newconfig" things.
				resources.MakeVirtualService2(
					"change-configs.default.example.com",
					simpleRunLatest("default", "change-configs", "newconfig"),
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
			},
			// Status updated to "newconfig"
			ExpectedStatus: v1alpha1.RouteStatus{
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
			},
		}, {
			Name:     "configuration missing",
			Context:  contextWithDefaultDomain("example.com"),
			Resource: simpleRunLatest("default", "config-missing", "not-found"),
			ExpectedStatus: v1alpha1.RouteStatus{
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
			},
		}, {
			Name:     "revision missing (direct)",
			Context:  contextWithDefaultDomain("example.com"),
			Resource: simplePinned("default", "missing-revision-direct", "not-found"),
			Objects: Objects{
				simpleReadyConfig("default", "config"),
			},
			ExpectedStatus: v1alpha1.RouteStatus{
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
			},
		}, {
			Name:     "revision missing (indirect)",
			Context:  contextWithDefaultDomain("example.com"),
			Resource: simpleRunLatest("default", "missing-revision-indirect", "config"),
			Objects: Objects{
				simpleReadyConfig("default", "config"),
			},
			ExpectedStatus: v1alpha1.RouteStatus{
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
			},
		}, {
			Name:    "pinned route becomes ready",
			Context: contextWithDefaultDomain("example.com"),
			Resource: simplePinned(
				"default",
				"pinned-becomes-ready",
				// Use the Revision name from the config
				simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
			),
			Objects: Objects{
				simpleReadyConfig("default", "config"),
				addOwnerRef(
					simpleReadyRevision("default",
						// Use the Revision name from the config.
						simpleReadyConfig("default", "config").Status.LatestReadyRevisionName,
					),
					or("Configuration", "config"),
				),
			},
			ExpectedCreates: Creates{
				resources.MakeVirtualService2(
					"pinned-becomes-ready.default.example.com",
					simpleRunLatest("default", "pinned-becomes-ready", "config"),
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
			// Use the config's revision name.
			ExpectedStatus: v1alpha1.RouteStatus{
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
			},
		}, {
			Name:    "traffic split becomes ready",
			Context: contextWithDefaultDomain("example.com"),
			Resource: routeWithTraffic("default", "named-traffic-split",
				v1alpha1.TrafficTarget{
					ConfigurationName: "blue",
					Percent:           50,
				}, v1alpha1.TrafficTarget{
					ConfigurationName: "green",
					Percent:           50,
				}),
			Objects: Objects{
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
			ExpectedCreates: Creates{
				resources.MakeVirtualService2(
					"named-traffic-split.default.example.com",
					routeWithTraffic("default", "named-traffic-split",
						v1alpha1.TrafficTarget{
							ConfigurationName: "blue",
							Percent:           50,
						}, v1alpha1.TrafficTarget{
							ConfigurationName: "green",
							Percent:           50,
						}),
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
			ExpectedStatus: v1alpha1.RouteStatus{
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
			},
		}, {
			Name: "change route configuration",
			// Start from a steady state referencing "blue", and modify the route spec to point to "green" instead.
			Context:  contextWithDefaultDomain("example.com"),
			Resource: simpleRunLatest("default", "switch-configs", "green"),
			Objects: Objects{
				simpleReadyConfig("default", "blue"),
				simpleReadyConfig("default", "green"),
				simpleReadyRevision("default",
					// Use the Revision name from the config.
					simpleReadyConfig("default", "blue").Status.LatestReadyRevisionName,
				),
				simpleReadyRevision("default",
					// Use the Revision name from the config.
					simpleReadyConfig("default", "green").Status.LatestReadyRevisionName,
				),
				resources.MakeVirtualService2(
					"switch-configs.default.example.com",
					simpleRunLatest("default", "switch-configs", "blue"),
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
				resources.MakeK8sService(simpleRunLatest("default", "switch-configs", "blue")),
			},
			ExpectedUpdates: Updates{
				resources.MakeVirtualService2(
					"switch-configs.default.example.com",
					simpleRunLatest("default", "switch-configs", "green"),
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
			},
			ExpectedStatus: v1alpha1.RouteStatus{
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
			},
		}}

	scenarios.Run(t, PhaseSetup(NewVirtualService))
}

// TODO(dprotaso)Review this alternate phase scenario invocation
//func TestVirtualServiceReconcile_FailureLabellingConfiguration(t *testing.T) {
//	oldRoute := simpleRunLatest("default", "addlabel-config-failure", "blue")
//
//	blueConfig := simpleReadyConfig("default", "blue")
//	greenConfig := simpleReadyConfig("default", "green")
//
//	blueRevision := simpleReadyRevision("default", blueConfig.Status.LatestCreatedRevisionName)
//	greenRevision := simpleReadyRevision("default", greenConfig.Status.LatestCreatedRevisionName)
//
//	virtualService := resources.MakeVirtualService2(
//		"addlabel-config-failure.default.example.com",
//		oldRoute,
//		&traffic.TrafficConfig{
//			Targets: map[string][]traffic.RevisionTarget{
//				"": {{
//					TrafficTarget: v1alpha1.TrafficTarget{
//						// Use the Revision name from the config.
//						RevisionName: simpleReadyConfig("default", "blue").Status.LatestReadyRevisionName,
//						Percent:      100,
//					},
//					Active: true,
//				}},
//			},
//		},
//	)
//
//	scenario := PhaseTest{
//		Name: "failure labeling configuration",
//		// Start from our test that switches configs, unlabel "blue" (avoids induced failure),
//		// and induce a failure when we go to label the "green" configuration.
//		ExpectError: true,
//		Failures: Failures{
//			InduceFailure("patch", "configurations"),
//		},
//		Context:  contextWithDefaultDomain("example.com"),
//		Resource: simpleRunLatest("default", "addlabel-config-failure", "green"),
//		Objects: Objects{
//			blueConfig,
//			blueRevision,
//			greenConfig,
//			greenRevision,
//			virtualService,
//		},
//		ExpectedPatches: Patches{
//			patchAddLabel("default", "green", "serving.knative.dev/route", "addlabel-config-failure", "v1"),
//		},
//		ExpectedStatus: v1alpha1.RouteStatus{},
//	}
//
//	scenario.Run(t, VSPhaseSetup, VirtualService{})
//}

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

func mutateVirtualService(vs *istiov1alpha3.VirtualService) *istiov1alpha3.VirtualService {
	// Thor's Hammer
	vs.Spec = istiov1alpha3.VirtualServiceSpec{}
	return vs
}

func simplePinned(namespace, name, revision string) *v1alpha1.Route {
	return routeWithTraffic(namespace, name, v1alpha1.TrafficTarget{
		RevisionName: revision,
		Percent:      100,
	})
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
