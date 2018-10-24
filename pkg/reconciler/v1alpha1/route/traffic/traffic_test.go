/*
Copyright 2018 The Knative Author

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
	"fmt"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	testNamespace       string = "test"
	defaultDomainSuffix string = "test-domain.dev"
	prodDomainSuffix    string = "prod-domain.com"
)

// A simple fixed Configuration/Revision layout for testing.
// Tests should not modify these objects.
var (
	// These are objects never inserted.
	missingConfig *v1alpha1.Configuration
	missingRev    *v1alpha1.Revision

	// emptyConfig never has any revision.
	emptyConfig *v1alpha1.Configuration

	// revDeletedConfig has a Ready revision but was deleted.
	revDeletedConfig *v1alpha1.Configuration

	// unreadyConfig only has unreadyRev, and it's not ready.
	unreadyConfig *v1alpha1.Configuration
	unreadyRev    *v1alpha1.Revision

	// failedConfig only has failedRev, and it fails to be ready.
	failedConfig *v1alpha1.Configuration
	failedRev    *v1alpha1.Revision

	// inactiveConfig only has inactiveRevision, and it's not active.
	inactiveConfig *v1alpha1.Configuration
	inactiveRev    *v1alpha1.Revision

	// goodConfig has two good revisions: goodOldRev and goodNewRev
	goodConfig *v1alpha1.Configuration
	goodOldRev *v1alpha1.Revision
	goodNewRev *v1alpha1.Revision

	// niceConfig has two good revisions: niceOldRev and niceNewRev
	niceConfig *v1alpha1.Configuration
	niceOldRev *v1alpha1.Revision
	niceNewRev *v1alpha1.Revision

	configLister listers.ConfigurationLister
	revLister    listers.RevisionLister
)

func setUp() {
	emptyConfig = getTestEmptyConfig("empty")
	revDeletedConfig = getTestConfigWithDeletedRevision("latest-rev-deleted")
	unreadyConfig, unreadyRev = getTestUnreadyConfig("unready")
	failedConfig, failedRev = getTestFailedConfig("failed")
	inactiveConfig, inactiveRev = getTestInactiveConfig("inactive")
	goodConfig, goodOldRev, goodNewRev = getTestReadyConfig("good")
	niceConfig, niceOldRev, niceNewRev = getTestReadyConfig("nice")
	servingClient := fakeclientset.NewSimpleClientset()

	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)
	configInformer := servingInformer.Serving().V1alpha1().Configurations()
	configLister = configInformer.Lister()
	revInformer := servingInformer.Serving().V1alpha1().Revisions()
	revLister = revInformer.Lister()

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
		case *v1alpha1.Configuration:
			configInformer.Informer().GetIndexer().Add(o)
		case *v1alpha1.Revision:
			revInformer.Informer().GetIndexer().Add(o)
		}
	}

	missingConfig, missingRev = getTestUnreadyConfig("missing")
}

// The vanilla use case of 100% directing to latest ready revision of a single configuration.
func TestBuildTrafficConfiguration_Vanilla(t *testing.T) {
	tts := []v1alpha1.TrafficTarget{{
		ConfigurationName: goodConfig.Name,
		Percent:           100,
	}}
	expected := &TrafficConfig{
		Targets: map[string][]RevisionTarget{
			"": {{
				TrafficTarget: v1alpha1.TrafficTarget{
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					Percent:           100,
				},
				Active: true,
			}},
		},
		RevisionTargets: []RevisionTarget{{
			TrafficTarget: v1alpha1.TrafficTarget{
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodNewRev.Name,
				Percent:           100,
			},
			Active: true,
		}},
		Configurations: map[string]*v1alpha1.Configuration{goodConfig.Name: goodConfig},
		Revisions:      map[string]*v1alpha1.Revision{goodNewRev.Name: goodNewRev},
	}
	if tc, err := BuildTrafficConfiguration(configLister, revLister, getTestRouteWithTrafficTargets(tts)); err != nil {
		t.Errorf("Unexpected error %v", err)
	} else if diff := cmp.Diff(expected, tc); diff != "" {
		t.Errorf("Unexpected traffic diff (-want +got): %v", diff)
	}
}

// The vanilla use case of 100% directing to latest revision of an inactive configuration.
func TestBuildTrafficConfiguration_VanillaScaledToZero(t *testing.T) {
	tts := []v1alpha1.TrafficTarget{{
		ConfigurationName: inactiveConfig.Name,
		Percent:           100,
	}}
	expected := &TrafficConfig{
		Targets: map[string][]RevisionTarget{
			"": {{
				TrafficTarget: v1alpha1.TrafficTarget{
					ConfigurationName: inactiveConfig.Name,
					RevisionName:      inactiveRev.Name,
					Percent:           100,
				},
				Active: false,
			}},
		},
		RevisionTargets: []RevisionTarget{{
			TrafficTarget: v1alpha1.TrafficTarget{
				ConfigurationName: inactiveConfig.Name,
				RevisionName:      inactiveRev.Name,
				Percent:           100,
			},
			Active: false,
		}},
		Configurations: map[string]*v1alpha1.Configuration{inactiveConfig.Name: inactiveConfig},
		Revisions:      map[string]*v1alpha1.Revision{inactiveRev.Name: inactiveRev},
	}
	if tc, err := BuildTrafficConfiguration(configLister, revLister, getTestRouteWithTrafficTargets(tts)); err != nil {
		t.Errorf("Unexpected error: %v", err)
	} else if diff := cmp.Diff(expected, tc); diff != "" {
		t.Errorf("Unexpected traffic diff (-want +got): %v", diff)
	}
}

// Transitioning from one good config to another by splitting traffic.
func TestBuildTrafficConfiguration_TwoConfigs(t *testing.T) {
	tts := []v1alpha1.TrafficTarget{{
		ConfigurationName: niceConfig.Name,
		Percent:           90,
	}, {
		ConfigurationName: goodConfig.Name,
		Percent:           10,
	}}
	expected := &TrafficConfig{
		Targets: map[string][]RevisionTarget{
			"": {{
				TrafficTarget: v1alpha1.TrafficTarget{
					ConfigurationName: niceConfig.Name,
					RevisionName:      niceNewRev.Name,
					Percent:           90,
				},
				Active: true}, {
				TrafficTarget: v1alpha1.TrafficTarget{
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					Percent:           10,
				},
				Active: true,
			}},
		},
		RevisionTargets: []RevisionTarget{{
			TrafficTarget: v1alpha1.TrafficTarget{
				ConfigurationName: niceConfig.Name,
				RevisionName:      niceNewRev.Name,
				Percent:           90,
			},
			Active: true}, {
			TrafficTarget: v1alpha1.TrafficTarget{
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodNewRev.Name,
				Percent:           10,
			},
			Active: true,
		}},
		Configurations: map[string]*v1alpha1.Configuration{goodConfig.Name: goodConfig, niceConfig.Name: niceConfig},
		Revisions:      map[string]*v1alpha1.Revision{goodNewRev.Name: goodNewRev, niceNewRev.Name: niceNewRev},
	}
	if tc, err := BuildTrafficConfiguration(configLister, revLister, getTestRouteWithTrafficTargets(tts)); err != nil {
		t.Errorf("Unexpected error %v", err)
	} else if diff := cmp.Diff(expected, tc); diff != "" {
		t.Errorf("Unexpected traffic diff (-want +got): %v", diff)
	}
}

// Splitting traffic between a fixed revision and the latest revision (canary).
func TestBuildTrafficConfiguration_Canary(t *testing.T) {
	tts := []v1alpha1.TrafficTarget{{
		RevisionName: goodOldRev.Name,
		Percent:      90,
	}, {
		ConfigurationName: goodConfig.Name,
		Percent:           10,
	}}
	expected := &TrafficConfig{
		Targets: map[string][]RevisionTarget{
			"": {{
				TrafficTarget: v1alpha1.TrafficTarget{
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodOldRev.Name,
					Percent:           90,
				},
				Active: true,
			}, {
				TrafficTarget: v1alpha1.TrafficTarget{
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					Percent:           10,
				},
				Active: true,
			}},
		},
		RevisionTargets: []RevisionTarget{{
			TrafficTarget: v1alpha1.TrafficTarget{
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodOldRev.Name,
				Percent:           90,
			},
			Active: true,
		}, {
			TrafficTarget: v1alpha1.TrafficTarget{
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodNewRev.Name,
				Percent:           10,
			},
			Active: true,
		}},
		Configurations: map[string]*v1alpha1.Configuration{goodConfig.Name: goodConfig},
		Revisions:      map[string]*v1alpha1.Revision{goodOldRev.Name: goodOldRev, goodNewRev.Name: goodNewRev},
	}
	if tc, err := BuildTrafficConfiguration(configLister, revLister, getTestRouteWithTrafficTargets(tts)); err != nil {
		t.Errorf("Unexpected error %v", err)
	} else if diff := cmp.Diff(expected, tc); diff != "" {
		t.Errorf("Unexpected traffic diff (-want +got): %v", diff)
	}
}

// Splitting traffic between latest revision and a fixed revision which is also latest.
func TestBuildTrafficConfiguration_Consolidated(t *testing.T) {
	tts := []v1alpha1.TrafficTarget{{
		Name:         "one",
		RevisionName: goodOldRev.Name,
		Percent:      49,
	}, {
		Name:         "two",
		RevisionName: goodNewRev.Name,
		Percent:      50,
	}, {
		Name:              "also-two",
		ConfigurationName: goodConfig.Name,
		Percent:           1,
	}}
	expected := &TrafficConfig{
		Targets: map[string][]RevisionTarget{
			"": {{
				TrafficTarget: v1alpha1.TrafficTarget{
					Name:              "one",
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodOldRev.Name,
					Percent:           49,
				},
				Active: true,
			}, {
				TrafficTarget: v1alpha1.TrafficTarget{
					Name:              "two",
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					Percent:           51,
				},
				Active: true,
			}},
			"one": {{
				TrafficTarget: v1alpha1.TrafficTarget{
					Name:              "one",
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodOldRev.Name,
					Percent:           100,
				},
				Active: true,
			}},
			"two": {{
				TrafficTarget: v1alpha1.TrafficTarget{
					Name:              "two",
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					Percent:           100,
				},
				Active: true,
			}},
			"also-two": {{
				TrafficTarget: v1alpha1.TrafficTarget{
					Name:              "also-two",
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					Percent:           100,
				},
				Active: true,
			}},
		},
		RevisionTargets: []RevisionTarget{{
			TrafficTarget: v1alpha1.TrafficTarget{
				Name:              "one",
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodOldRev.Name,
				Percent:           49,
			},
			Active: true,
		}, {
			TrafficTarget: v1alpha1.TrafficTarget{
				Name:              "two",
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodNewRev.Name,
				Percent:           50,
			},
			Active: true,
		}, {
			TrafficTarget: v1alpha1.TrafficTarget{
				Name:              "also-two",
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodNewRev.Name,
				Percent:           1,
			},
			Active: true,
		}},
		Configurations: map[string]*v1alpha1.Configuration{goodConfig.Name: goodConfig},
		Revisions:      map[string]*v1alpha1.Revision{goodOldRev.Name: goodOldRev, goodNewRev.Name: goodNewRev},
	}
	if tc, err := BuildTrafficConfiguration(configLister, revLister, getTestRouteWithTrafficTargets(tts)); err != nil {
		t.Errorf("Unexpected error %v", err)
	} else if diff := cmp.Diff(expected, tc); diff != "" {
		t.Errorf("Unexpected traffic diff (-want +got): %v", diff)
	}
}

// Splitting traffic between a two fixed revisions.
func TestBuildTrafficConfiguration_TwoFixedRevisions(t *testing.T) {
	tts := []v1alpha1.TrafficTarget{{
		RevisionName: goodOldRev.Name,
		Percent:      90,
	}, {
		RevisionName: goodNewRev.Name,
		Percent:      10,
	}}
	expected := &TrafficConfig{
		Targets: map[string][]RevisionTarget{
			"": {{
				TrafficTarget: v1alpha1.TrafficTarget{
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodOldRev.Name,
					Percent:           90,
				},
				Active: true,
			}, {
				TrafficTarget: v1alpha1.TrafficTarget{
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					Percent:           10,
				},
				Active: true,
			}},
		},
		RevisionTargets: []RevisionTarget{{
			TrafficTarget: v1alpha1.TrafficTarget{
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodOldRev.Name,
				Percent:           90,
			},
			Active: true,
		}, {
			TrafficTarget: v1alpha1.TrafficTarget{
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodNewRev.Name,
				Percent:           10,
			},
			Active: true,
		}},
		Configurations: map[string]*v1alpha1.Configuration{goodConfig.Name: goodConfig},
		Revisions:      map[string]*v1alpha1.Revision{goodNewRev.Name: goodNewRev, goodOldRev.Name: goodOldRev},
	}
	if tc, err := BuildTrafficConfiguration(configLister, revLister, getTestRouteWithTrafficTargets(tts)); err != nil {
		t.Errorf("Unexpected error %v", err)
	} else if diff := cmp.Diff(expected, tc); diff != "" {
		t.Errorf("Unexpected traffic diff (-want +got): %v", diff)
	}
}

// Splitting traffic between a two fixed revisions of two configurations.
func TestBuildTrafficConfiguration_TwoFixedRevisionsFromTwoConfigurations(t *testing.T) {
	tts := []v1alpha1.TrafficTarget{{
		RevisionName: goodNewRev.Name,
		Percent:      40,
	}, {
		RevisionName: niceNewRev.Name,
		Percent:      60,
	}}
	expected := &TrafficConfig{
		Targets: map[string][]RevisionTarget{
			"": {{
				TrafficTarget: v1alpha1.TrafficTarget{
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					Percent:           40,
				},
				Active: true,
			}, {
				TrafficTarget: v1alpha1.TrafficTarget{
					ConfigurationName: niceConfig.Name,
					RevisionName:      niceNewRev.Name,
					Percent:           60,
				},
				Active: true,
			}},
		},
		RevisionTargets: []RevisionTarget{{
			TrafficTarget: v1alpha1.TrafficTarget{
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodNewRev.Name,
				Percent:           40,
			},
			Active: true,
		}, {
			TrafficTarget: v1alpha1.TrafficTarget{
				ConfigurationName: niceConfig.Name,
				RevisionName:      niceNewRev.Name,
				Percent:           60,
			},
			Active: true,
		}},
		Configurations: map[string]*v1alpha1.Configuration{goodConfig.Name: goodConfig, niceConfig.Name: niceConfig},
		Revisions:      map[string]*v1alpha1.Revision{goodNewRev.Name: goodNewRev, niceNewRev.Name: niceNewRev},
	}
	if tc, err := BuildTrafficConfiguration(configLister, revLister, getTestRouteWithTrafficTargets(tts)); err != nil {
		t.Errorf("Unexpected error %v", err)
	} else if diff := cmp.Diff(expected, tc); diff != "" {
		t.Errorf("Unexpected traffic diff (-want +got): %v", diff)
	}
}

// One fixed, two named targets for newer stuffs.
func TestBuildTrafficConfiguration_Preliminary(t *testing.T) {
	tts := []v1alpha1.TrafficTarget{{
		RevisionName: goodOldRev.Name,
		Percent:      100,
	}, {
		Name:         "beta",
		RevisionName: goodNewRev.Name,
	}, {
		Name:              "alpha",
		ConfigurationName: niceConfig.Name,
	}}
	expected := &TrafficConfig{
		Targets: map[string][]RevisionTarget{
			"": {{
				TrafficTarget: v1alpha1.TrafficTarget{
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodOldRev.Name,
					Percent:           100,
				},
				Active: true}, {
				TrafficTarget: v1alpha1.TrafficTarget{
					Name:              "beta",
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
				},
				Active: true}, {
				TrafficTarget: v1alpha1.TrafficTarget{
					Name:              "alpha",
					ConfigurationName: niceConfig.Name,
					RevisionName:      niceNewRev.Name,
				},
				Active: true,
			}},
			"beta": {{
				TrafficTarget: v1alpha1.TrafficTarget{
					Name:              "beta",
					ConfigurationName: goodConfig.Name,
					RevisionName:      goodNewRev.Name,
					Percent:           100,
				},
				Active: true}},
			"alpha": {{
				TrafficTarget: v1alpha1.TrafficTarget{
					Name:              "alpha",
					ConfigurationName: niceConfig.Name,
					RevisionName:      niceNewRev.Name,
					Percent:           100,
				},
				Active: true,
			}},
		},
		RevisionTargets: []RevisionTarget{{
			TrafficTarget: v1alpha1.TrafficTarget{
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodOldRev.Name,
				Percent:           100,
			},
			Active: true}, {
			TrafficTarget: v1alpha1.TrafficTarget{
				Name:              "beta",
				ConfigurationName: goodConfig.Name,
				RevisionName:      goodNewRev.Name,
			},
			Active: true}, {
			TrafficTarget: v1alpha1.TrafficTarget{
				Name:              "alpha",
				ConfigurationName: niceConfig.Name,
				RevisionName:      niceNewRev.Name,
			},
			Active: true,
		}},
		Configurations: map[string]*v1alpha1.Configuration{goodConfig.Name: goodConfig, niceConfig.Name: niceConfig},
		Revisions:      map[string]*v1alpha1.Revision{goodOldRev.Name: goodOldRev, goodNewRev.Name: goodNewRev, niceNewRev.Name: niceNewRev},
	}
	if tc, err := BuildTrafficConfiguration(configLister, revLister, getTestRouteWithTrafficTargets(tts)); err != nil {
		t.Errorf("Unexpected error %v", err)
	} else if diff := cmp.Diff(expected, tc); diff != "" {
		t.Errorf("Unexpected traffic diff (-want +got): %v", diff)
	}
}

func TestBuildTrafficConfiguration_MissingConfig(t *testing.T) {
	tts := []v1alpha1.TrafficTarget{{
		RevisionName: goodOldRev.Name,
		Percent:      100,
	}, {
		Name:         "beta",
		RevisionName: goodNewRev.Name,
	}, {
		Name:              "alpha",
		ConfigurationName: missingConfig.Name,
	}}
	expected := &TrafficConfig{
		Targets:        map[string][]RevisionTarget{},
		Configurations: map[string]*v1alpha1.Configuration{goodConfig.Name: goodConfig},
		Revisions:      map[string]*v1alpha1.Revision{goodOldRev.Name: goodOldRev, goodNewRev.Name: goodNewRev},
	}
	expectedErr := errMissingConfiguration(missingConfig.Name)
	r := getTestRouteWithTrafficTargets(tts)
	tc, err := BuildTrafficConfiguration(configLister, revLister, r)
	if expectedErr.Error() != err.Error() {
		t.Errorf("Expected %v, saw %v", expectedErr, err)
	}
	if diff := cmp.Diff(expected, tc); diff != "" {
		t.Errorf("Unexpected traffic diff (-want +got): %v", diff)
	}
}

func TestBuildTrafficConfiguration_NotRoutableRevision(t *testing.T) {
	tts := []v1alpha1.TrafficTarget{{
		RevisionName: unreadyRev.Name,
		Percent:      100,
	}}
	expected := &TrafficConfig{
		Targets:        map[string][]RevisionTarget{},
		Configurations: map[string]*v1alpha1.Configuration{},
		Revisions:      map[string]*v1alpha1.Revision{unreadyRev.Name: unreadyRev},
	}
	expectedErr := errUnreadyRevision(unreadyRev)
	r := getTestRouteWithTrafficTargets(tts)
	tc, err := BuildTrafficConfiguration(configLister, revLister, r)
	if expectedErr.Error() != err.Error() {
		t.Errorf("Expected error %v, saw %v", expectedErr, err)
	}
	if diff := cmp.Diff(expected, tc); diff != "" {
		t.Errorf("Unexpected traffic diff (-want +got): %v", diff)
	}
}

func TestBuildTrafficConfiguration_NotRoutableConfiguration(t *testing.T) {
	tts := []v1alpha1.TrafficTarget{{
		ConfigurationName: unreadyConfig.Name,
		Percent:           100,
	}}
	expected := &TrafficConfig{
		Targets:        map[string][]RevisionTarget{},
		Configurations: map[string]*v1alpha1.Configuration{unreadyConfig.Name: unreadyConfig},
		Revisions:      map[string]*v1alpha1.Revision{},
	}
	expectedErr := errUnreadyConfiguration(unreadyConfig)
	r := getTestRouteWithTrafficTargets(tts)
	tc, err := BuildTrafficConfiguration(configLister, revLister, r)
	if expectedErr.Error() != err.Error() {
		t.Errorf("Expected error %v, saw %v", expectedErr, err)
	}
	if diff := cmp.Diff(expected, tc); diff != "" {
		t.Errorf("Unexpected traffic diff (-want +got): %v", diff)
	}
}

func TestBuildTrafficConfiguration_EmptyConfiguration(t *testing.T) {
	tts := []v1alpha1.TrafficTarget{{
		ConfigurationName: emptyConfig.Name,
		Percent:           100,
	}}
	expected := &TrafficConfig{
		Targets:        map[string][]RevisionTarget{},
		Configurations: map[string]*v1alpha1.Configuration{emptyConfig.Name: emptyConfig},
		Revisions:      map[string]*v1alpha1.Revision{},
	}
	expectedErr := errUnreadyConfiguration(emptyConfig)
	r := getTestRouteWithTrafficTargets(tts)
	tc, err := BuildTrafficConfiguration(configLister, revLister, r)
	if expectedErr.Error() != err.Error() {
		t.Errorf("Expected error %v, saw %v", expectedErr, err)
	}
	if diff := cmp.Diff(expected, tc); diff != "" {
		t.Errorf("Unexpected traffic diff (-want +got): %v", diff)
	}
}

func TestBuildTrafficConfiguration_EmptyAndFailedConfigurations(t *testing.T) {
	tts := []v1alpha1.TrafficTarget{{
		ConfigurationName: emptyConfig.Name,
		Percent:           50,
	}, {
		ConfigurationName: failedConfig.Name,
		Percent:           50,
	}}
	expected := &TrafficConfig{
		Targets: map[string][]RevisionTarget{},
		Configurations: map[string]*v1alpha1.Configuration{
			emptyConfig.Name:  emptyConfig,
			failedConfig.Name: failedConfig,
		},
		Revisions: map[string]*v1alpha1.Revision{},
	}
	expectedErr := errUnreadyConfiguration(failedConfig)
	r := getTestRouteWithTrafficTargets(tts)
	tc, err := BuildTrafficConfiguration(configLister, revLister, r)
	if expectedErr.Error() != err.Error() {
		t.Errorf("Expected error %v, saw %v", expectedErr, err)
	}
	if diff := cmp.Diff(expected, tc); diff != "" {
		t.Errorf("Unexpected traffic diff (-want +got): %v", diff)
	}
}

func TestBuildTrafficConfiguration_FailedAndEmptyConfigurations(t *testing.T) {
	tts := []v1alpha1.TrafficTarget{{
		ConfigurationName: failedConfig.Name,
		Percent:           50,
	}, {
		ConfigurationName: emptyConfig.Name,
		Percent:           50,
	}}
	expected := &TrafficConfig{
		Targets: map[string][]RevisionTarget{},
		Configurations: map[string]*v1alpha1.Configuration{
			emptyConfig.Name:  emptyConfig,
			failedConfig.Name: failedConfig,
		},
		Revisions: map[string]*v1alpha1.Revision{},
	}
	expectedErr := errUnreadyConfiguration(failedConfig)
	r := getTestRouteWithTrafficTargets(tts)
	tc, err := BuildTrafficConfiguration(configLister, revLister, r)
	if expectedErr.Error() != err.Error() {
		t.Errorf("Expected error %v, saw %v", expectedErr, err)
	}
	if diff := cmp.Diff(expected, tc); diff != "" {
		t.Errorf("Unexpected traffic diff (-want +got): %v", diff)
	}
}

func TestBuildTrafficConfiguration_MissingRevision(t *testing.T) {
	tts := []v1alpha1.TrafficTarget{{
		RevisionName: missingRev.Name,
		Percent:      50,
	}, {
		RevisionName: goodNewRev.Name,
		Percent:      50,
	}}
	expected := &TrafficConfig{
		Targets:        map[string][]RevisionTarget{},
		Configurations: map[string]*v1alpha1.Configuration{goodConfig.Name: goodConfig},
		Revisions:      map[string]*v1alpha1.Revision{goodNewRev.Name: goodNewRev},
	}
	expectedErr := errMissingRevision(missingRev.Name)
	r := getTestRouteWithTrafficTargets(tts)
	tc, err := BuildTrafficConfiguration(configLister, revLister, r)
	if expectedErr.Error() != err.Error() {
		t.Errorf("Expected %s, saw %s", expectedErr.Error(), err.Error())
	}
	if diff := cmp.Diff(expected, tc); diff != "" {
		t.Errorf("Unexpected traffic diff (-want +got): %v", diff)
	}
}

func TestRoundTripping(t *testing.T) {
	tts := []v1alpha1.TrafficTarget{{
		RevisionName: goodOldRev.Name,
		Percent:      100,
	}, {
		Name:         "beta",
		RevisionName: goodNewRev.Name,
	}, {
		Name:              "alpha",
		ConfigurationName: niceConfig.Name,
	}}
	expected := []v1alpha1.TrafficTarget{{
		RevisionName: goodOldRev.Name,
		Percent:      100,
	}, {
		Name:         "beta",
		RevisionName: goodNewRev.Name,
	}, {
		Name:         "alpha",
		RevisionName: niceNewRev.Name,
	}}
	if tc, err := BuildTrafficConfiguration(configLister, revLister, getTestRouteWithTrafficTargets(tts)); err != nil {
		t.Errorf("Unexpected error %v", err)
	} else if diff := cmp.Diff(expected, tc.GetRevisionTrafficTargets()); diff != "" {
		fmt.Printf("%+v\n", tc.GetRevisionTrafficTargets())
		t.Errorf("Unexpected traffic diff (-want +got): %v", diff)
	}
}

func getTestConfig(name string) *v1alpha1.Configuration {
	return &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: v1alpha1.ConfigurationSpec{
			// This is a workaround for generation initialization
			Generation: 1,
			RevisionTemplate: v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					Container: corev1.Container{
						Image: "test-image",
					},
				},
			},
		},
	}
}

func getTestRevForConfig(config *v1alpha1.Configuration, name string) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels: map[string]string{
				serving.ConfigurationLabelKey: config.Name,
			},
		},
		Spec: *config.Spec.RevisionTemplate.Spec.DeepCopy(),
	}
}

func getTestRouteWithTrafficTargets(traffic []v1alpha1.TrafficTarget) *v1alpha1.Route {
	return &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: testNamespace,
			Labels: map[string]string{
				"route": "test-route",
			},
		},
		Spec: v1alpha1.RouteSpec{
			Traffic: traffic,
		},
	}
}

func getTestEmptyConfig(name string) *v1alpha1.Configuration {
	config := getTestConfig(name + "-config")
	config.Status.InitializeConditions()
	return config
}

func getTestConfigWithDeletedRevision(name string) *v1alpha1.Configuration {
	config := getTestConfig(name + "-config")
	config.Status.SetLatestCreatedRevisionName("i-was-deleted")
	config.Status.SetLatestReadyRevisionName("")
	config.Status.MarkLatestReadyDeleted()
	return config
}

func getTestUnreadyConfig(name string) (*v1alpha1.Configuration, *v1alpha1.Revision) {
	config := getTestConfig(name + "-config")
	rev := getTestRevForConfig(config, name+"-revision")
	config.Status.SetLatestCreatedRevisionName(rev.Name)
	return config, rev
}

func getTestFailedConfig(name string) (*v1alpha1.Configuration, *v1alpha1.Revision) {
	config := getTestConfig(name + "-config")
	rev := getTestRevForConfig(config, name+"-revision")
	config.Status.SetLatestCreatedRevisionName(rev.Name)
	config.Status.MarkLatestCreatedFailed(rev.Name, "Permanently failed")
	rev.Status.MarkContainerMissing("Should have used ko")
	return config, rev
}

func getTestInactiveConfig(name string) (*v1alpha1.Configuration, *v1alpha1.Revision) {
	config := getTestConfig(name + "-config")
	rev := getTestRevForConfig(config, name+"-revision")
	config.Status.SetLatestReadyRevisionName(rev.Name)
	config.Status.SetLatestCreatedRevisionName(rev.Name)
	rev.Status.InitializeConditions()
	rev.Status.MarkInactive("Reserve", "blah blah blah")
	return config, rev
}

func getTestReadyConfig(name string) (*v1alpha1.Configuration, *v1alpha1.Revision, *v1alpha1.Revision) {
	config := getTestConfig(name + "-config")
	rev1 := getTestRevForConfig(config, name+"-revision-1")
	rev1.Status.MarkResourcesAvailable()
	rev1.Status.MarkContainerHealthy()
	rev1.Status.MarkActive()
	rev2 := getTestRevForConfig(config, name+"-revision-2")
	rev2.Status.MarkResourcesAvailable()
	rev2.Status.MarkContainerHealthy()
	rev2.Status.MarkActive()
	config.Status.SetLatestReadyRevisionName(rev2.Name)
	config.Status.SetLatestCreatedRevisionName(rev2.Name)
	return config, rev1, rev2
}

func TestMain(m *testing.M) {
	setUp()
	code := m.Run()
	os.Exit(code)
}
