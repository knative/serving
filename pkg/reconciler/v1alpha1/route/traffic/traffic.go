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
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
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
// In the case that some target is missing, an error of type TargetError will be returned.
func BuildTrafficConfiguration(configLister listers.ConfigurationLister, revLister listers.RevisionLister,
	u *v1alpha1.Route) (*TrafficConfig, error) {
	builder := newBuilder(configLister, revLister, u.Namespace)
	for _, tt := range u.Spec.Traffic {
		if err := builder.addTrafficTarget(&tt); err != nil {
			// Other non-traffic target errors shouldn't be ignored.
			return nil, err
		}
	}
	return builder.build()
}

// GetRevisionTrafficTargets return a list of TrafficTarget flattened to the RevisionName, and having ConfigurationName cleared out.
func (t *TrafficConfig) GetRevisionTrafficTargets() []v1alpha1.TrafficTarget {
	results := []v1alpha1.TrafficTarget{}
	for _, tt := range t.Targets[""] {
		results = append(results, v1alpha1.TrafficTarget{ RevisionName:tt.RevisionName, Name:tt.Name, Percent:tt.Percent })
	}
	return results
}

type trafficConfigBuilder struct {
	configLister listers.ConfigurationLister
	revLister    listers.RevisionLister
	namespace    string

	// targets is a grouping of traffic targets serving the same origin.
	targets map[string][]RevisionTarget
	// configurations contains all the referred Configuration, keyed by their name.
	configurations map[string]*v1alpha1.Configuration
	// revisions contains all the referred Revision, keyed by their name.
	revisions map[string]*v1alpha1.Revision

	// TargetError are deferred until we got a complete list of all referred targets.
	deferredTargetErr TargetError
}

func newBuilder(configLister listers.ConfigurationLister, revLister listers.RevisionLister, namespace string) *trafficConfigBuilder {
	return &trafficConfigBuilder{
		configLister: configLister,
		revLister:    revLister,
		namespace:    namespace,
		targets:      make(map[string][]RevisionTarget),

		configurations: make(map[string]*v1alpha1.Configuration),
		revisions:      make(map[string]*v1alpha1.Revision),
	}
}

func (t *trafficConfigBuilder) getConfiguration(name string) (*v1alpha1.Configuration, error) {
	if _, ok := t.configurations[name]; !ok {
		config, err := t.configLister.Configurations(t.namespace).Get(name)
		if errors.IsNotFound(err) {
			return nil, errMissingConfiguration(name)
		} else if err != nil {
			return nil, err
		}
		t.configurations[name] = config
	}
	return t.configurations[name], nil
}

func (t *trafficConfigBuilder) getRevision(name string) (*v1alpha1.Revision, error) {
	if _, ok := t.revisions[name]; !ok {
		rev, err := t.revLister.Revisions(t.namespace).Get(name)
		if errors.IsNotFound(err) {
			return nil, errMissingRevision(name)
		} else if err != nil {
			return nil, err
		}
		t.revisions[name] = rev
	}
	return t.revisions[name], nil
}

// deferTargetError will record a TargetError.  A TargetError with
// IsFailure()=true will always overwrite a previous TargetError.
func (t *trafficConfigBuilder) deferTargetError(err TargetError) {
	if t.deferredTargetErr == nil || err.IsFailure() {
		t.deferredTargetErr = err
	}
}

func (t *trafficConfigBuilder) addTrafficTarget(tt *v1alpha1.TrafficTarget) error {
	var err error
	if tt.RevisionName != "" {
		err = t.addRevisionTarget(tt)
	} else if tt.ConfigurationName != "" {
		err = t.addConfigurationTarget(tt)
	}
	if err, ok := err.(TargetError); err != nil && ok {
		// Defer target errors, as we still want to compile a list of
		// all referred targets, including missing ones.
		t.deferTargetError(err)
		return nil
	}
	return err
}

// addConfigurationTarget flattens a traffic target to the Revision level, by looking up for the LatestReadyRevisionName
// on the referred Configuration.  It adds both to the lists of directly referred targets.
func (t *trafficConfigBuilder) addConfigurationTarget(tt *v1alpha1.TrafficTarget) error {
	config, err := t.getConfiguration(tt.ConfigurationName)
	if err != nil {
		return err
	}
	if config.Status.LatestReadyRevisionName == "" {
		return errUnreadyConfiguration(config)
	}
	rev, err := t.getRevision(config.Status.LatestReadyRevisionName)
	if err != nil {
		return err
	}
	target := RevisionTarget{
		TrafficTarget: *tt,
		Active:        !rev.Status.IsActivationRequired(),
	}
	target.TrafficTarget.RevisionName = rev.Name
	t.addFlattenedTarget(target)
	return nil
}

func (t *trafficConfigBuilder) addRevisionTarget(tt *v1alpha1.TrafficTarget) error {
	rev, err := t.getRevision(tt.RevisionName)
	if err != nil {
		return err
	}
	if !rev.Status.IsRoutable() {
		return errUnreadyRevision(rev)
	}
	target := RevisionTarget{
		TrafficTarget: *tt,
		Active:        !rev.Status.IsActivationRequired(),
	}
	t.revisions[tt.RevisionName] = rev
	if configName, ok := rev.Labels[serving.ConfigurationLabelKey]; ok {
		target.TrafficTarget.ConfigurationName = configName
		if _, err := t.getConfiguration(configName); err != nil {
			return err
		}
	}
	t.addFlattenedTarget(target)
	return nil
}

func (t *trafficConfigBuilder) addFlattenedTarget(target RevisionTarget) {
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

func (t *trafficConfigBuilder) build() (*TrafficConfig, error) {
	if t.deferredTargetErr != nil {
		t.targets = nil
	}
	return &TrafficConfig{
		Targets:        consolidateAll(t.targets),
		Configurations: t.configurations,
		Revisions:      t.revisions,
	}, t.deferredTargetErr
}
