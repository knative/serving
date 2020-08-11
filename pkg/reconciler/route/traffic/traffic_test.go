/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package traffic

import (
	"context"
	"errors"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	network "knative.dev/networking/pkg"
	net "knative.dev/networking/pkg/apis/networking"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	fakeclientset "knative.dev/serving/pkg/client/clientset/versioned/fake"
	informers "knative.dev/serving/pkg/client/informers/externalversions"
	listers "knative.dev/serving/pkg/client/listers/serving/v1"
	"knative.dev/serving/pkg/gc"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/domains"
	. "knative.dev/serving/pkg/testing/v1"
)

const testNamespace string = "test"

// A simple fixed Configuration/Revision layout for testing.
// Tests should not modify these objects.
var (
	// These are objects never inserted.
	missingConfig *v1.Configuration
	missingRev    *v1.Revision

	// emptyConfig never has any revision.
	emptyConfig *v1.Configuration

	// revDeletedConfig has a Ready revision but was deleted.
	revDeletedConfig *v1.Configuration

	// unreadyConfig only has unreadyRev, and it's not ready.
	unreadyConfig *v1.Configuration
	unreadyRev    *v1.Revision

	// failedConfig only has failedRev, and it fails to be ready.
	failedConfig *v1.Configuration
	failedRev    *v1.Revision

	// inactiveConfig only has inactiveRevision, and it's not active.
	inactiveConfig *v1.Configuration
	inactiveRev    *v1.Revision

	// goodConfig has two good revisions: goodOldRev and goodNewRev
	goodConfig *v1.Configuration
	goodOldRev *v1.Revision
	goodNewRev *v1.Revision

	// niceConfig has two good revisions: niceOldRev and niceNewRev
	niceConfig *v1.Configuration
	niceOldRev *v1.Revision
	niceNewRev *v1.Revision

	configLister listers.ConfigurationLister
	revLister    listers.RevisionLister

	// fake lister to return API error.
	revErrorLister listers.RevisionLister

	cmpOpts = []cmp.Option{cmp.AllowUnexported(Config{})}
)

func setUp() {
	emptyConfig = getTestEmptyConfig("empty")
	revDeletedConfig = testConfigWithDeletedRevision("latest-rev-deleted")
	unreadyConfig, unreadyRev = getTestUnreadyConfig("unready")
	failedConfig, failedRev = getTestFailedConfig("failed")
	inactiveConfig, inactiveRev = getTestInactiveConfig("inactive")
	goodConfig, goodOldRev, goodNewRev = getTestReadyConfig("good")
	niceConfig, niceOldRev, niceNewRev = getTestReadyConfig("nice")
	servingClient := fakeclientset.NewSimpleClientset()

	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)
	configInformer := servingInformer.Serving().V1().Configurations()
	configLister = configInformer.Lister()
	revInformer := servingInformer.Serving().V1().Revisions()
	revLister = revInformer.Lister()

	revErrorLister = revFakeErrorLister{}

	// Add these test objects to the informers.
	objs := []runtime.Object{
		unreadyConfig, unreadyRev,
		failedConfig, failedRev,
		inactiveConfig, inactiveRev,
		revDeletedConfig,
		emptyConfig,
		goodConfig, goodOldRev, goodNewRev,
		niceConfig, niceOldRev, niceNewRev,
	}

	for _, obj := range objs {
		switch o := obj.(type) {
		case *v1.Configuration:
			configInformer.Informer().GetIndexer().Add(o)
		case *v1.Revision:
			revInformer.Informer().GetIndexer().Add(o)
		}
	}

	missingConfig, missingRev = getTestUnreadyConfig("missing")
}

// The vanilla use case of 100% directing to latest ready revision of a single configuration.
func TestBuildTrafficConfigurationVanilla(t *testing.T) {
	tts := v1.TrafficTarget{
		ConfigurationName: goodConfig.Name,
		Percent:           ptr.Int64(100),
	}

	expected := &Config{
		Targets: map[string]RevisionTargets{
			DefaultTarget: {{
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					Percent:           ptr.Int64(100),
					LatestRevision:    ptr.Bool(true),
				},
				Active:   true,
				Protocol: net.ProtocolH2C,
			}},
		},
		revisionTargets: []RevisionTarget{{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodNewRev.Name,
				Percent:           ptr.Int64(100),
				LatestRevision:    ptr.Bool(true),
			},
			Active:   true,
			Protocol: net.ProtocolH2C,
		}},
		Configurations: map[string]*v1.Configuration{
			goodConfig.Name: goodConfig,
		},
		Revisions: map[string]*v1.Revision{
			goodNewRev.Name: goodNewRev,
		},
	}
	if tc, err := BuildTrafficConfiguration(configLister, revLister, testRouteWithTrafficTargets(WithSpecTraffic(tts))); err != nil {
		t.Errorf("Unexpected error %v", err)
	} else if got, want := tc, expected; !cmp.Equal(want, got, cmpOpts...) {
		t.Errorf("Unexpected traffic diff (-want +got): %v", cmp.Diff(want, got, cmpOpts...))
	}
}

func testRouteWithTrafficTargets(trafficTarget RouteOption) *v1.Route {
	return Route(testNamespace, "test-route",
		WithRouteLabel(map[string]string{"route": "test-route"}), trafficTarget)
}

func TestBuildTrafficConfigurationNoNameRevision(t *testing.T) {
	tts := v1.TrafficTarget{
		RevisionName: goodNewRev.Name,
		Percent:      ptr.Int64(100),
	}
	expected := &Config{
		Targets: map[string]RevisionTargets{
			DefaultTarget: {{
				TrafficTarget: v1.TrafficTarget{
					RevisionName:      goodNewRev.Name,
					ConfigurationName: goodConfig.Name,
					Percent:           ptr.Int64(100),
					LatestRevision:    ptr.Bool(false),
				},
				Active:   true,
				Protocol: net.ProtocolH2C,
			}},
		},
		revisionTargets: []RevisionTarget{{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodNewRev.Name,
				Percent:           ptr.Int64(100),
				LatestRevision:    ptr.Bool(false),
			},
			Active:   true,
			Protocol: net.ProtocolH2C,
		}},
		Configurations: map[string]*v1.Configuration{goodConfig.Name: goodConfig},
		Revisions:      map[string]*v1.Revision{goodNewRev.Name: goodNewRev},
	}
	if tc, err := BuildTrafficConfiguration(configLister, revLister, testRouteWithTrafficTargets(WithSpecTraffic(tts))); err != nil {
		t.Errorf("Unexpected error %v", err)
	} else if got, want := tc, expected; !cmp.Equal(want, got, cmpOpts...) {
		t.Errorf("Unexpected traffic diff (-want +got): %v", cmp.Diff(want, got, cmpOpts...))
	}
}

// The vanilla use case of 100% directing to latest revision of an inactive configuration.
func TestBuildTrafficConfigurationVanillaScaledToZero(t *testing.T) {
	tts := v1.TrafficTarget{
		ConfigurationName: inactiveConfig.Name,
		Percent:           ptr.Int64(100),
	}
	expected := &Config{
		Targets: map[string]RevisionTargets{
			DefaultTarget: {{
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: inactiveConfig.Name,
					RevisionName:      inactiveRev.Name,
					Percent:           ptr.Int64(100),
					LatestRevision:    ptr.Bool(true),
				},
				Active:   false,
				Protocol: net.ProtocolHTTP1,
			}},
		},
		revisionTargets: []RevisionTarget{{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: inactiveConfig.Name,
				RevisionName:      inactiveRev.Name,
				Percent:           ptr.Int64(100),
				LatestRevision:    ptr.Bool(true),
			},
			Active:   false,
			Protocol: net.ProtocolHTTP1,
		}},
		Configurations: map[string]*v1.Configuration{
			inactiveConfig.Name: inactiveConfig,
		},
		Revisions: map[string]*v1.Revision{
			inactiveRev.Name: inactiveRev,
		},
	}
	if tc, err := BuildTrafficConfiguration(configLister, revLister, testRouteWithTrafficTargets(WithSpecTraffic(tts))); err != nil {
		t.Errorf("Unexpected error %v", err)
	} else if got, want := tc, expected; !cmp.Equal(want, got, cmpOpts...) {
		t.Errorf("Unexpected traffic diff (-want +got): %v", cmp.Diff(want, got, cmpOpts...))
	}
}

// Transitioning from one good config to another by splitting traffic.
func TestBuildTrafficConfigurationTwoConfigs(t *testing.T) {
	expected := &Config{
		Targets: map[string]RevisionTargets{
			DefaultTarget: {{
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: niceConfig.Name,
					RevisionName:      niceNewRev.Name,
					Percent:           ptr.Int64(90),
					LatestRevision:    ptr.Bool(true),
				},
				Active:   true,
				Protocol: net.ProtocolH2C,
			}, {
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					Percent:           ptr.Int64(10),
					LatestRevision:    ptr.Bool(true),
				},
				Active:   true,
				Protocol: net.ProtocolH2C,
			}},
		},
		revisionTargets: []RevisionTarget{{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: niceConfig.Name,
				RevisionName:      niceNewRev.Name,
				Percent:           ptr.Int64(90),
				LatestRevision:    ptr.Bool(true),
			},
			Active:   true,
			Protocol: net.ProtocolH2C,
		}, {
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodNewRev.Name,
				Percent:           ptr.Int64(10),
				LatestRevision:    ptr.Bool(true),
			},
			Active:   true,
			Protocol: net.ProtocolH2C,
		}},
		Configurations: map[string]*v1.Configuration{
			goodConfig.Name: goodConfig,
			niceConfig.Name: niceConfig,
		},
		Revisions: map[string]*v1.Revision{
			goodNewRev.Name: goodNewRev,
			niceNewRev.Name: niceNewRev,
		},
	}
	if tc, err := BuildTrafficConfiguration(configLister, revLister, testRouteWithTrafficTargets(WithSpecTraffic(v1.TrafficTarget{
		ConfigurationName: niceConfig.Name,
		Percent:           ptr.Int64(90),
	}, v1.TrafficTarget{
		ConfigurationName: goodConfig.Name,
		Percent:           ptr.Int64(10),
	}))); err != nil {
		t.Errorf("Unexpected error %v", err)
	} else if got, want := tc, expected; !cmp.Equal(want, got, cmpOpts...) {
		t.Errorf("Unexpected traffic diff (-want +got): %v", cmp.Diff(want, got, cmpOpts...))
	}
}

// Splitting traffic between a fixed revision and the latest revision (canary).
func TestBuildTrafficConfigurationCanary(t *testing.T) {
	expected := &Config{
		Targets: map[string]RevisionTargets{
			DefaultTarget: {{
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodOldRev.Name,
					Percent:           ptr.Int64(90),
					LatestRevision:    ptr.Bool(false),
				},
				Active:   true,
				Protocol: net.ProtocolHTTP1,
			}, {
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					Percent:           ptr.Int64(10),
					LatestRevision:    ptr.Bool(true),
				},
				Active:   true,
				Protocol: net.ProtocolH2C,
			}},
		},
		revisionTargets: []RevisionTarget{{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodOldRev.Name,
				Percent:           ptr.Int64(90),
				LatestRevision:    ptr.Bool(false),
			},
			Active:   true,
			Protocol: net.ProtocolHTTP1,
		}, {
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodNewRev.Name,
				Percent:           ptr.Int64(10),
				LatestRevision:    ptr.Bool(true),
			},
			Active:   true,
			Protocol: net.ProtocolH2C,
		}},
		Configurations: map[string]*v1.Configuration{
			goodConfig.Name: goodConfig,
		},
		Revisions: map[string]*v1.Revision{
			goodOldRev.Name: goodOldRev,
			goodNewRev.Name: goodNewRev,
		},
	}
	if tc, err := BuildTrafficConfiguration(configLister, revLister, testRouteWithTrafficTargets(WithSpecTraffic(v1.TrafficTarget{
		RevisionName: goodOldRev.Name,
		Percent:      ptr.Int64(90),
	}, v1.TrafficTarget{
		ConfigurationName: goodConfig.Name,
		Percent:           ptr.Int64(10),
	}))); err != nil {
		t.Errorf("Unexpected error %v", err)
	} else if got, want := tc, expected; !cmp.Equal(want, got, cmpOpts...) {
		t.Errorf("Unexpected traffic diff (-want +got): %v", cmp.Diff(want, got, cmpOpts...))
	}
}

// Splitting traffic between latest revision and a fixed revision which is also latest.
func TestBuildTrafficConfigurationConsolidated(t *testing.T) {
	expected := &Config{
		Targets: map[string]RevisionTargets{
			DefaultTarget: {{
				TrafficTarget: v1.TrafficTarget{
					Tag:               "one",
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodOldRev.Name,
					Percent:           ptr.Int64(49),
					LatestRevision:    ptr.Bool(false),
				},
				Active:   true,
				Protocol: net.ProtocolHTTP1,
			}, {
				TrafficTarget: v1.TrafficTarget{
					Tag:               "two",
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					Percent:           ptr.Int64(51),
					LatestRevision:    ptr.Bool(false),
				},
				Active:   true,
				Protocol: net.ProtocolH2C,
			}},
			"one": {{
				TrafficTarget: v1.TrafficTarget{
					Tag:               "one",
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodOldRev.Name,
					Percent:           ptr.Int64(100),
					LatestRevision:    ptr.Bool(false),
				},
				Active:   true,
				Protocol: net.ProtocolHTTP1,
			}},
			"two": {{
				TrafficTarget: v1.TrafficTarget{
					Tag:               "two",
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					Percent:           ptr.Int64(100),
					LatestRevision:    ptr.Bool(false),
				},
				Active:   true,
				Protocol: net.ProtocolH2C,
			}},
			"also-two": {{
				TrafficTarget: v1.TrafficTarget{
					Tag:               "also-two",
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					Percent:           ptr.Int64(100),
					LatestRevision:    ptr.Bool(true),
				},
				Active:   true,
				Protocol: net.ProtocolH2C,
			}},
		},
		revisionTargets: []RevisionTarget{{
			TrafficTarget: v1.TrafficTarget{
				Tag:               "one",
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodOldRev.Name,
				Percent:           ptr.Int64(49),
				LatestRevision:    ptr.Bool(false),
			},
			Active:   true,
			Protocol: net.ProtocolHTTP1,
		}, {
			TrafficTarget: v1.TrafficTarget{
				Tag:               "two",
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodNewRev.Name,
				Percent:           ptr.Int64(50),
				LatestRevision:    ptr.Bool(false),
			},
			Active:   true,
			Protocol: net.ProtocolH2C,
		}, {
			TrafficTarget: v1.TrafficTarget{
				Tag:               "also-two",
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodNewRev.Name,
				Percent:           ptr.Int64(1),
				LatestRevision:    ptr.Bool(true),
			},
			Active:   true,
			Protocol: net.ProtocolH2C,
		}},
		Configurations: map[string]*v1.Configuration{
			goodConfig.Name: goodConfig,
		},
		Revisions: map[string]*v1.Revision{
			goodOldRev.Name: goodOldRev,
			goodNewRev.Name: goodNewRev,
		},
	}
	if tc, err := BuildTrafficConfiguration(configLister, revLister, testRouteWithTrafficTargets(WithSpecTraffic(v1.TrafficTarget{
		Tag:          "one",
		RevisionName: goodOldRev.Name,
		Percent:      ptr.Int64(49),
	}, v1.TrafficTarget{
		Tag:          "two",
		RevisionName: goodNewRev.Name,
		Percent:      ptr.Int64(50),
	}, v1.TrafficTarget{
		Tag:               "also-two",
		ConfigurationName: goodConfig.Name,
		Percent:           ptr.Int64(1),
	}))); err != nil {
		t.Errorf("Unexpected error %v", err)
	} else if got, want := tc, expected; !cmp.Equal(want, got, cmpOpts...) {
		t.Errorf("Unexpected traffic diff (-want +got): %v", cmp.Diff(want, got, cmpOpts...))
	}
}

// Splitting traffic between a two fixed revisions.
func TestBuildTrafficConfigurationTwoFixedRevisions(t *testing.T) {
	expected := &Config{
		Targets: map[string]RevisionTargets{
			DefaultTarget: {{
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodOldRev.Name,
					Percent:           ptr.Int64(90),
					LatestRevision:    ptr.Bool(false),
				},
				Active:   true,
				Protocol: net.ProtocolHTTP1,
			}, {
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					Percent:           ptr.Int64(10),
					LatestRevision:    ptr.Bool(false),
				},
				Active:   true,
				Protocol: net.ProtocolH2C,
			}},
		},
		revisionTargets: []RevisionTarget{{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodOldRev.Name,
				Percent:           ptr.Int64(90),
				LatestRevision:    ptr.Bool(false),
			},
			Active:   true,
			Protocol: net.ProtocolHTTP1,
		}, {
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodNewRev.Name,
				Percent:           ptr.Int64(10),
				LatestRevision:    ptr.Bool(false),
			},
			Active:   true,
			Protocol: net.ProtocolH2C,
		}},
		Configurations: map[string]*v1.Configuration{
			goodConfig.Name: goodConfig,
		},
		Revisions: map[string]*v1.Revision{
			goodNewRev.Name: goodNewRev,
			goodOldRev.Name: goodOldRev,
		},
	}
	if tc, err := BuildTrafficConfiguration(configLister, revLister, testRouteWithTrafficTargets(WithSpecTraffic(v1.TrafficTarget{
		RevisionName: goodOldRev.Name,
		Percent:      ptr.Int64(90),
	}, v1.TrafficTarget{
		RevisionName: goodNewRev.Name,
		Percent:      ptr.Int64(10),
	}))); err != nil {
		t.Errorf("Unexpected error %v", err)
	} else if got, want := tc, expected; !cmp.Equal(want, got, cmpOpts...) {
		t.Errorf("Unexpected traffic diff (-want +got): %v", cmp.Diff(want, got, cmpOpts...))
	}
}

// Splitting traffic between a two fixed revisions of two configurations.
func TestBuildTrafficConfigurationTwoFixedRevisionsFromTwoConfigurations(t *testing.T) {
	expected := &Config{
		Targets: map[string]RevisionTargets{
			DefaultTarget: {{
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					Percent:           ptr.Int64(40),
					LatestRevision:    ptr.Bool(false),
				},
				Active:   true,
				Protocol: net.ProtocolH2C,
			}, {
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: niceConfig.Name,
					RevisionName:      niceNewRev.Name,
					Percent:           ptr.Int64(60),
					LatestRevision:    ptr.Bool(false),
				},
				Active:   true,
				Protocol: net.ProtocolH2C,
			}},
		},
		revisionTargets: []RevisionTarget{{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodNewRev.Name,
				Percent:           ptr.Int64(40),
				LatestRevision:    ptr.Bool(false),
			},
			Active:   true,
			Protocol: net.ProtocolH2C,
		}, {
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: niceConfig.Name,
				RevisionName:      niceNewRev.Name,
				Percent:           ptr.Int64(60),
				LatestRevision:    ptr.Bool(false),
			},
			Active:   true,
			Protocol: net.ProtocolH2C,
		}},
		Configurations: map[string]*v1.Configuration{
			goodConfig.Name: goodConfig,
			niceConfig.Name: niceConfig,
		},
		Revisions: map[string]*v1.Revision{
			goodNewRev.Name: goodNewRev,
			niceNewRev.Name: niceNewRev,
		},
	}
	if tc, err := BuildTrafficConfiguration(configLister, revLister, testRouteWithTrafficTargets(WithSpecTraffic(v1.TrafficTarget{
		RevisionName: goodNewRev.Name,
		Percent:      ptr.Int64(40),
	}, v1.TrafficTarget{
		RevisionName: niceNewRev.Name,
		Percent:      ptr.Int64(60),
	}))); err != nil {
		t.Errorf("Unexpected error %v", err)
	} else if got, want := tc, expected; !cmp.Equal(want, got, cmpOpts...) {
		t.Errorf("Unexpected traffic diff (-want +got): %v", cmp.Diff(want, got, cmpOpts...))
	}
}

// One fixed, two named targets for newer stuffs.
func TestBuildTrafficConfigurationPreliminary(t *testing.T) {
	expected := &Config{
		Targets: map[string]RevisionTargets{
			DefaultTarget: {{
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodOldRev.Name,
					Percent:           ptr.Int64(100),
					LatestRevision:    ptr.Bool(false),
				},
				Active:   true,
				Protocol: net.ProtocolHTTP1,
			}, {
				TrafficTarget: v1.TrafficTarget{
					Tag:               "beta",
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					LatestRevision:    ptr.Bool(false),
				},
				Active:   true,
				Protocol: net.ProtocolH2C,
			}, {
				TrafficTarget: v1.TrafficTarget{
					Tag:               "alpha",
					ConfigurationName: niceConfig.Name,
					RevisionName:      niceNewRev.Name,
					LatestRevision:    ptr.Bool(true),
				},
				Active:   true,
				Protocol: net.ProtocolH2C,
			}},
			"beta": {{
				TrafficTarget: v1.TrafficTarget{
					Tag:               "beta",
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					Percent:           ptr.Int64(100),
					LatestRevision:    ptr.Bool(false),
				},
				Active:   true,
				Protocol: net.ProtocolH2C,
			}},
			"alpha": {{
				TrafficTarget: v1.TrafficTarget{
					Tag:               "alpha",
					ConfigurationName: niceConfig.Name,
					RevisionName:      niceNewRev.Name,
					Percent:           ptr.Int64(100),
					LatestRevision:    ptr.Bool(true),
				},
				Active:   true,
				Protocol: net.ProtocolH2C,
			}},
		},
		revisionTargets: []RevisionTarget{{
			TrafficTarget: v1.TrafficTarget{
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodOldRev.Name,
				Percent:           ptr.Int64(100),
				LatestRevision:    ptr.Bool(false),
			},
			Active:   true,
			Protocol: net.ProtocolHTTP1,
		}, {
			TrafficTarget: v1.TrafficTarget{
				Tag:               "beta",
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodNewRev.Name,
				LatestRevision:    ptr.Bool(false),
			},
			Active:   true,
			Protocol: net.ProtocolH2C,
		}, {
			TrafficTarget: v1.TrafficTarget{
				Tag:               "alpha",
				ConfigurationName: niceConfig.Name,
				RevisionName:      niceNewRev.Name,
				LatestRevision:    ptr.Bool(true),
			},
			Active:   true,
			Protocol: net.ProtocolH2C,
		}},
		Configurations: map[string]*v1.Configuration{
			goodConfig.Name: goodConfig,
			niceConfig.Name: niceConfig,
		},
		Revisions: map[string]*v1.Revision{
			goodOldRev.Name: goodOldRev,
			goodNewRev.Name: goodNewRev,
			niceNewRev.Name: niceNewRev,
		},
	}
	if tc, err := BuildTrafficConfiguration(configLister, revLister,
		testRouteWithTrafficTargets(WithSpecTraffic(v1.TrafficTarget{
			RevisionName: goodOldRev.Name,
			Percent:      ptr.Int64(100),
		}, v1.TrafficTarget{
			Tag:          "beta",
			RevisionName: goodNewRev.Name,
		}, v1.TrafficTarget{
			Tag:               "alpha",
			ConfigurationName: niceConfig.Name,
		}))); err != nil {
		t.Error("Unexpected error:", err)
	} else if got, want := tc, expected; !cmp.Equal(want, got, cmpOpts...) {
		t.Errorf("Unexpected traffic diff (-want +got):\n%s", cmp.Diff(want, got, cmpOpts...))
	}
}

func TestBuildTrafficConfigurationMissingConfig(t *testing.T) {
	expected := &Config{
		Targets: map[string]RevisionTargets{},
		Configurations: map[string]*v1.Configuration{
			goodConfig.Name: goodConfig,
		},
		Revisions: map[string]*v1.Revision{
			goodOldRev.Name: goodOldRev,
			goodNewRev.Name: goodNewRev,
		},
		MissingTargets: []corev1.ObjectReference{{
			APIVersion: "serving.knative.dev/v1",
			Kind:       "Configuration",
			Name:       missingConfig.Name,
			Namespace:  missingConfig.Namespace,
		}},
	}

	expectedErr := errMissingConfiguration(missingConfig.Name)
	r := testRouteWithTrafficTargets(WithSpecTraffic(v1.TrafficTarget{
		RevisionName: goodOldRev.Name,
		Percent:      ptr.Int64(100),
	}, v1.TrafficTarget{
		Tag:          "beta",
		RevisionName: goodNewRev.Name,
	}, v1.TrafficTarget{
		Tag:               "alpha",
		ConfigurationName: missingConfig.Name,
	}))
	if tc, err := BuildTrafficConfiguration(configLister, revLister, r); err != nil && expectedErr.Error() != err.Error() {
		t.Errorf("Expected %v, saw %v", expectedErr, err)
	} else if got, want := tc, expected; !cmp.Equal(want, got, cmpOpts...) {
		t.Errorf("Unexpected traffic diff (-want +got): %v", cmp.Diff(want, got, cmpOpts...))
	}
}

func TestBuildTrafficConfigurationNotRoutableRevision(t *testing.T) {
	expected := &Config{
		Targets:        map[string]RevisionTargets{},
		Configurations: map[string]*v1.Configuration{},
		Revisions:      map[string]*v1.Revision{unreadyRev.Name: unreadyRev},
	}
	expectedErr := errUnreadyRevision(unreadyRev)
	if tc, err := BuildTrafficConfiguration(configLister, revLister, testRouteWithTrafficTargets(WithSpecTraffic(v1.TrafficTarget{
		RevisionName: unreadyRev.Name,
		Percent:      ptr.Int64(100),
	}))); err != nil && expectedErr.Error() != err.Error() {
		t.Errorf("Expected error %v, saw %v", expectedErr, err)
	} else if got, want := tc, expected; !cmp.Equal(want, got, cmpOpts...) {
		t.Errorf("Unexpected traffic diff (-want +got): %v", cmp.Diff(want, got, cmpOpts...))
	}
}

func TestBuildTrafficConfigurationNotRoutableConfiguration(t *testing.T) {
	expected := &Config{
		Targets:        map[string]RevisionTargets{},
		Configurations: map[string]*v1.Configuration{unreadyConfig.Name: unreadyConfig},
		Revisions:      map[string]*v1.Revision{},
	}
	expectedErr := errUnreadyConfiguration(unreadyConfig)
	if tc, err := BuildTrafficConfiguration(configLister, revLister, testRouteWithTrafficTargets(WithSpecTraffic(v1.TrafficTarget{
		ConfigurationName: unreadyConfig.Name,
		Percent:           ptr.Int64(100),
	}))); err != nil && expectedErr.Error() != err.Error() {
		t.Errorf("Expected error %v, saw %v", expectedErr, err)
	} else if got, want := tc, expected; !cmp.Equal(want, got, cmpOpts...) {
		t.Errorf("Unexpected traffic diff (-want +got): %v", cmp.Diff(want, got, cmpOpts...))
	}
}

func TestBuildTrafficConfigurationEmptyConfiguration(t *testing.T) {
	expected := &Config{
		Targets: map[string]RevisionTargets{},
		Configurations: map[string]*v1.Configuration{
			emptyConfig.Name: emptyConfig,
		},
		Revisions: map[string]*v1.Revision{},
	}

	expectedErr := errUnreadyConfiguration(emptyConfig)
	if tc, err := BuildTrafficConfiguration(configLister, revLister, testRouteWithTrafficTargets(WithSpecTraffic(v1.TrafficTarget{
		ConfigurationName: emptyConfig.Name,
		Percent:           ptr.Int64(100),
	}))); err != nil && expectedErr.Error() != err.Error() {
		t.Errorf("Expected error %v, saw %v", expectedErr, err)
	} else if got, want := tc, expected; !cmp.Equal(want, got, cmpOpts...) {
		t.Errorf("Unexpected traffic diff (-want +got): %v", cmp.Diff(want, got, cmpOpts...))
	}
}

func TestBuildTrafficConfigurationEmptyAndFailedConfigurations(t *testing.T) {
	expected := &Config{
		Targets: map[string]RevisionTargets{},
		Configurations: map[string]*v1.Configuration{
			emptyConfig.Name:  emptyConfig,
			failedConfig.Name: failedConfig,
		},
		Revisions: map[string]*v1.Revision{},
	}
	expectedErr := errUnreadyConfiguration(failedConfig)
	if tc, err := BuildTrafficConfiguration(configLister, revLister, testRouteWithTrafficTargets(WithSpecTraffic(v1.TrafficTarget{
		ConfigurationName: emptyConfig.Name,
		Percent:           ptr.Int64(50),
	}, v1.TrafficTarget{
		ConfigurationName: failedConfig.Name,
		Percent:           ptr.Int64(50),
	}))); err != nil && expectedErr.Error() != err.Error() {
		t.Errorf("Expected error %v, saw %v", expectedErr, err)
	} else if got, want := tc, expected; !cmp.Equal(want, got, cmpOpts...) {
		t.Errorf("Unexpected traffic diff (-want +got): %v", cmp.Diff(want, got, cmpOpts...))
	}
}

func TestBuildTrafficConfigurationFailedAndEmptyConfigurations(t *testing.T) {
	expected := &Config{
		Targets: map[string]RevisionTargets{},
		Configurations: map[string]*v1.Configuration{
			emptyConfig.Name:  emptyConfig,
			failedConfig.Name: failedConfig,
		},
		Revisions: map[string]*v1.Revision{},
	}
	expectedErr := errUnreadyConfiguration(failedConfig)
	if tc, err := BuildTrafficConfiguration(configLister, revLister, testRouteWithTrafficTargets(WithSpecTraffic(v1.TrafficTarget{
		ConfigurationName: failedConfig.Name,
		Percent:           ptr.Int64(50),
	}, v1.TrafficTarget{
		ConfigurationName: emptyConfig.Name,
		Percent:           ptr.Int64(50),
	}))); err != nil && expectedErr.Error() != err.Error() {
		t.Errorf("Expected error %v, saw %v", expectedErr, err)
	} else if got, want := tc, expected; !cmp.Equal(want, got, cmpOpts...) {
		t.Errorf("Unexpected traffic diff (-want +got): %v", cmp.Diff(want, got, cmpOpts...))
	}
}

func TestBuildTrafficConfigurationMissingRevision(t *testing.T) {
	expected := &Config{
		Targets:        map[string]RevisionTargets{},
		Configurations: map[string]*v1.Configuration{goodConfig.Name: goodConfig},
		Revisions:      map[string]*v1.Revision{goodNewRev.Name: goodNewRev},
		MissingTargets: []corev1.ObjectReference{{
			APIVersion: "serving.knative.dev/v1",
			Kind:       "Revision",
			Name:       missingRev.Name,
			Namespace:  missingRev.Namespace,
		}},
	}
	expectedErr := errMissingRevision(missingRev.Name)
	if tc, err := BuildTrafficConfiguration(configLister, revLister, testRouteWithTrafficTargets(WithSpecTraffic(v1.TrafficTarget{
		RevisionName: missingRev.Name,
		Percent:      ptr.Int64(50),
	}, v1.TrafficTarget{
		RevisionName: goodNewRev.Name,
		Percent:      ptr.Int64(50),
	}))); err != nil && expectedErr.Error() != err.Error() {
		t.Errorf("Expected %s, saw %s", expectedErr.Error(), err.Error())
	} else if got, want := tc, expected; !cmp.Equal(want, got, cmpOpts...) {
		t.Errorf("Unexpected traffic diff (-want +got): %v", cmp.Diff(want, got, cmpOpts...))
	}
}

var errAPI = errors.New("failed to connect API")

type revFakeErrorLister struct{}

func (l revFakeErrorLister) Get(name string) (*v1.Revision, error) {
	return nil, errAPI
}

func (l revFakeErrorLister) List(selector labels.Selector) ([]*v1.Revision, error) {
	log.Panic("not implemented")
	return nil, nil
}

func (l revFakeErrorLister) Revisions(namespace string) listers.RevisionNamespaceLister {
	return l
}

func TestBuildTrafficConfigurationFailedGetRevision(t *testing.T) {
	_, err := BuildTrafficConfiguration(configLister, revErrorLister, testRouteWithTrafficTargets(WithSpecTraffic(v1.TrafficTarget{
		RevisionName: goodNewRev.Name,
		Percent:      ptr.Int64(50)})))
	if err != nil && err.Error() != errAPI.Error() {
		t.Errorf("err: %s, want: %s", err.Error(), errAPI.Error())
	} else if err == nil {
		t.Errorf("err: %s, want: no error", errAPI.Error())
	}
}

func TestRoundTripping(t *testing.T) {
	expected := []v1.TrafficTarget{{
		RevisionName:   goodOldRev.Name,
		Percent:        ptr.Int64(100),
		LatestRevision: ptr.Bool(false),
	}, {
		Tag:            "beta",
		RevisionName:   goodNewRev.Name,
		URL:            domains.URL(domains.HTTPScheme, "beta-test-route.test.example.com"),
		LatestRevision: ptr.Bool(false),
	}, {
		Tag:            "alpha",
		RevisionName:   niceNewRev.Name,
		URL:            domains.URL(domains.HTTPScheme, "alpha-test-route.test.example.com"),
		LatestRevision: ptr.Bool(true),
	}}
	route := testRouteWithTrafficTargets(WithSpecTraffic(v1.TrafficTarget{
		RevisionName: goodOldRev.Name,
		Percent:      ptr.Int64(100),
	}, v1.TrafficTarget{
		Tag:          "beta",
		RevisionName: goodNewRev.Name,
	}, v1.TrafficTarget{
		Tag:               "alpha",
		ConfigurationName: niceConfig.Name,
	}))
	if tc, err := BuildTrafficConfiguration(configLister, revLister, route); err != nil {
		t.Errorf("Unexpected error %v", err)
	} else {
		targets, err := tc.GetRevisionTrafficTargets(getContext(), route)
		if err != nil {
			t.Error("Unexpected error:", err)
		}
		if got, want := targets, expected; !cmp.Equal(want, got) {
			t.Errorf("Unexpected traffic diff (-want +got):\n%s", cmp.Diff(want, got))
		}
	}
}

func testConfig(name string) *v1.Configuration {
	return &v1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: v1.ConfigurationSpec{
			Template: v1.RevisionTemplateSpec{
				Spec: v1.RevisionSpec{
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Image: "test-image",
						}},
					},
				},
			},
		},
	}
}

func testRevForConfig(config *v1.Configuration, name string) *v1.Revision {
	return &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels: map[string]string{
				serving.ConfigurationLabelKey: config.Name,
			},
		},
		Spec: *config.Spec.GetTemplate().Spec.DeepCopy(),
	}
}

func getTestEmptyConfig(name string) *v1.Configuration {
	config := testConfig(name + "-config")
	config.Status.InitializeConditions()
	return config
}

func testConfigWithDeletedRevision(name string) *v1.Configuration {
	config := testConfig(name + "-config")
	config.Status.SetLatestCreatedRevisionName("i-was-deleted")
	config.Status.SetLatestReadyRevisionName("")
	config.Status.MarkLatestReadyDeleted()
	return config
}

func getTestUnreadyConfig(name string) (*v1.Configuration, *v1.Revision) {
	config := testConfig(name + "-config")
	rev := testRevForConfig(config, name+"-revision")
	config.Status.SetLatestCreatedRevisionName(rev.Name)
	return config, rev
}

func getTestFailedConfig(name string) (*v1.Configuration, *v1.Revision) {
	config := testConfig(name + "-config")
	rev := testRevForConfig(config, name+"-revision")
	config.Status.SetLatestCreatedRevisionName(rev.Name)
	config.Status.MarkLatestCreatedFailed(rev.Name, "Permanently failed")
	rev.Status.MarkContainerHealthyFalse(v1.ReasonContainerMissing, "Should have used ko")
	return config, rev
}

func getTestInactiveConfig(name string) (*v1.Configuration, *v1.Revision) {
	config := testConfig(name + "-config")
	rev := testRevForConfig(config, name+"-revision")
	config.Status.SetLatestReadyRevisionName(rev.Name)
	config.Status.SetLatestCreatedRevisionName(rev.Name)
	rev.Status.InitializeConditions()
	rev.Status.MarkActiveFalse("Reserve", "blah blah blah")
	return config, rev
}

func getTestReadyConfig(name string) (*v1.Configuration, *v1.Revision, *v1.Revision) {
	config := testConfig(name + "-config")
	rev1 := testRevForConfig(config, name+"-revision-1")
	rev1.Status.MarkResourcesAvailableTrue()
	rev1.Status.MarkContainerHealthyTrue()
	rev1.Status.MarkActiveTrue()

	// rev1 will use http1, rev2 will use h2c
	config.Spec.GetTemplate().Spec.GetContainer().Ports = []corev1.ContainerPort{{
		Name: "h2c",
	}}

	rev2 := testRevForConfig(config, name+"-revision-2")
	rev2.Status.MarkResourcesAvailableTrue()
	rev2.Status.MarkContainerHealthyTrue()
	rev2.Status.MarkActiveTrue()
	config.Status.SetLatestReadyRevisionName(rev2.Name)
	config.Status.SetLatestCreatedRevisionName(rev2.Name)
	return config, rev1, rev2
}

func TestMain(m *testing.M) {
	setUp()
	os.Exit(m.Run())
}

func getContext() context.Context {
	ctx := context.Background()
	cfg := testNetworkConfig()
	return config.ToContext(ctx, cfg)
}

func testNetworkConfig() *config.Config {
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
			DefaultIngressClass: "test-ingress-class",
			DomainTemplate:      network.DefaultDomainTemplate,
			TagTemplate:         network.DefaultTagTemplate,
		},
		GC: &gc.Config{
			StaleRevisionLastpinnedDebounce: 1 * time.Minute,
		},
	}
}
