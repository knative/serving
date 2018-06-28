/*
Copyright 2018 Google LLC

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

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	client "github.com/knative/serving/pkg/client/clientset/versioned/typed/serving/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// A RevisionTarget adds the Active/Inactive state of a Revision to a flattened TrafficTarget.
type RevisionTarget struct {
	v1alpha1.TrafficTarget
	Active bool
}

// TrafficConfig encapsulates details of our traffic so that we don't need to make API calls, or use details of the
// route beyond its ObjectMeta to make routing changes.
type TrafficConfig struct {
	// The traffic targets, flattened to the Revision-level.
	Targets map[string][]RevisionTarget

	// The referred Configurations and Revisions.
	Configurations map[string]*v1alpha1.Configuration
	Revisions      map[string]*v1alpha1.Revision
}

// BuildTrafficConfiguration consolidates and flattens the Route.Spec.Traffic to the Revision-level. It also provides a
// complete lists of Configurations and Revisions referred by the Route, directly or indirectly.  These referred targets
// are keyed by name for easy access.
//
// In the case that some target is missing, it sets the Route.Status to TrafficNotAssigned with a reference to the
// missing target, and return one of the error.  However, it still returns a complete list of referred existing targets.
func BuildTrafficConfiguration(configClient client.ConfigurationInterface, revClient client.RevisionInterface, u *v1alpha1.Route) (*TrafficConfig, error) {
	builder := newBuilder(configClient, revClient)
	var lastErr error
	for _, tt := range u.Spec.Traffic {
		if tt.RevisionName != "" {
			if err := builder.addRevisionTarget(&tt); err != nil {
				u.Status.MarkTrafficNotAssigned("Revision", tt.RevisionName)
				lastErr = err
			}
		} else if tt.ConfigurationName != "" {
			if err := builder.addConfigurationTarget(&tt); err != nil {
				u.Status.MarkTrafficNotAssigned("Configuration", tt.ConfigurationName)
				lastErr = err
			}
		}
	}
	if lastErr != nil {
		builder.targets = nil
	}
	return builder.build(), lastErr
}

// GetTrafficTargets returns a list of TrafficTarget.
func (t *TrafficConfig) GetTrafficTargets() []v1alpha1.TrafficTarget {
	results := []v1alpha1.TrafficTarget{}
	for _, tt := range t.Targets[""] {
		results = append(results, tt.TrafficTarget)
	}
	return results
}

type trafficConfigBuilder struct {
	configClient client.ConfigurationInterface
	revClient    client.RevisionInterface

	targets map[string][]RevisionTarget

	configurations map[string]*v1alpha1.Configuration
	revisions      map[string]*v1alpha1.Revision
}

func newBuilder(configClient client.ConfigurationInterface, revClient client.RevisionInterface) *trafficConfigBuilder {
	return &trafficConfigBuilder{
		configClient: configClient,
		revClient:    revClient,
		targets:      make(map[string][]RevisionTarget),

		configurations: make(map[string]*v1alpha1.Configuration),
		revisions:      make(map[string]*v1alpha1.Revision),
	}
}

func (t *trafficConfigBuilder) getConfiguration(name string) (*v1alpha1.Configuration, error) {
	if _, ok := t.configurations[name]; !ok {
		config, err := t.configClient.Get(name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		t.configurations[name] = config
	}
	return t.configurations[name], nil
}

func (t *trafficConfigBuilder) getRevision(name string) (*v1alpha1.Revision, error) {
	if _, ok := t.revisions[name]; !ok {
		rev, err := t.revClient.Get(name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		t.revisions[name] = rev
	}
	return t.revisions[name], nil
}

// addConfigurationTarget flattens a traffic target to the Revision level, by looking up for the LatestReadyRevisionName
// on the referred Configuration.  It adds both to the lists of directly referred targets.
func (t *trafficConfigBuilder) addConfigurationTarget(tt *v1alpha1.TrafficTarget) error {
	config, err := t.getConfiguration(tt.ConfigurationName)
	if err != nil {
		return err
	}
	target := RevisionTarget{TrafficTarget: *tt}
	if config.Status.LatestReadyRevisionName == "" {
		return fmt.Errorf("Configuration %q is not ready", config.Name)
	}
	rev, err := t.getRevision(config.Status.LatestReadyRevisionName)
	if err != nil {
		return err
	}
	target.TrafficTarget.RevisionName = rev.Name
	target.Active = !rev.Status.IsActivationRequired()
	t.addTarget(target)
	return nil
}

func (t *trafficConfigBuilder) addRevisionTarget(tt *v1alpha1.TrafficTarget) error {
	target := RevisionTarget{TrafficTarget: *tt}
	rev, err := t.getRevision(tt.RevisionName)
	if err != nil {
		return err
	}
	if !rev.Status.IsRoutable() {
		return fmt.Errorf("Revision %q is not routable", rev.Name)
	}
	t.revisions[tt.RevisionName] = rev
	target.Active = !rev.Status.IsActivationRequired()
	if configName, ok := rev.Labels[serving.ConfigurationLabelKey]; ok {
		target.TrafficTarget.ConfigurationName = configName
		if _, err := t.getConfiguration(configName); err != nil {
			return err
		}
	}
	t.addTarget(target)
	return nil
}

func (t *trafficConfigBuilder) addTarget(target RevisionTarget) {
	name := target.TrafficTarget.Name
	t.targets[""] = append(t.targets[""], target)
	if name != "" {
		t.targets[name] = append(t.targets[name], target)
	}
}

func consolidate(targets []RevisionTarget) []RevisionTarget {
	byName := make(map[string]RevisionTarget)
	names := []string{}
	for _, tt := range targets {
		name := tt.TrafficTarget.RevisionName
		cur, ok := byName[name]
		if !ok {
			byName[name] = tt
			names = append(names, name)
		} else {
			cur.TrafficTarget.Percent += tt.TrafficTarget.Percent
			byName[name] = cur
		}
	}
	consolidated := []RevisionTarget{}
	for _, name := range names {
		consolidated = append(consolidated, byName[name])
	}
	if len(consolidated) == 1 {
		consolidated[0].TrafficTarget.Percent = 100
	}
	return consolidated
}

func consolidateAll(targets map[string][]RevisionTarget) map[string][]RevisionTarget {
	consolidated := make(map[string][]RevisionTarget)
	for name, tts := range targets {
		consolidated[name] = consolidate(tts)
	}
	return consolidated
}

func (t *trafficConfigBuilder) build() *TrafficConfig {
	return &TrafficConfig{
		Targets:        consolidateAll(t.targets),
		Configurations: t.configurations,
		Revisions:      t.revisions,
	}
}
