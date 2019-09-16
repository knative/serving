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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"

	"knative.dev/pkg/ptr"
	net "knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	listers "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
	"knative.dev/serving/pkg/reconciler/route/domains"
	"knative.dev/serving/pkg/reconciler/route/resources/labels"
)

const (
	// DefaultTarget is the unnamed default target for the traffic.
	DefaultTarget = ""
)

// A RevisionTarget adds the Active/Inactive state and the transport protocol of a
// Revision to a flattened TrafficTarget.
type RevisionTarget struct {
	v1.TrafficTarget
	Active      bool
	Protocol    net.ProtocolType
	ServiceName string // Revision service name.
}

// RevisionTargets is a collection of revision targets.
type RevisionTargets []RevisionTarget

// Config encapsulates details of our traffic so that we don't need to make API calls, or use details of the
// route beyond its ObjectMeta to make routing changes.
type Config struct {
	// Group of traffic splits.  Un-named targets are grouped together
	// under the key `DefaultTarget`, and named target are under the respective
	// name.  This is used to configure network configuration to
	// realize a route's setting.
	Targets map[string]RevisionTargets

	// A list traffic targets, flattened to the Revision level.  This
	// is used to populate the Route.Status.TrafficTarget field.
	revisionTargets RevisionTargets

	// The referred `Configuration`s and `Revision`s.
	Configurations map[string]*v1alpha1.Configuration
	Revisions      map[string]*v1alpha1.Revision
}

// BuildTrafficConfiguration consolidates and flattens the Route.Spec.Traffic to the Revision-level. It also provides a
// complete lists of Configurations and Revisions referred by the Route, directly or indirectly.  These referred targets
// are keyed by name for easy access.
//
// In the case that some target is missing, an error of type TargetError will be returned.
func BuildTrafficConfiguration(configLister listers.ConfigurationLister, revLister listers.RevisionLister,
	r *v1alpha1.Route) (*Config, error) {
	builder := newBuilder(configLister, revLister, r.Namespace, len(r.Spec.Traffic))
	builder.applySpecTraffic(r.Spec.Traffic)
	return builder.build()
}

// GetRevisionTrafficTargets returns a list of TrafficTarget flattened to the RevisionName, and having ConfigurationName cleared out.
func (t *Config) GetRevisionTrafficTargets(ctx context.Context, r *v1alpha1.Route, clusterLocalService sets.String) ([]v1alpha1.TrafficTarget, error) {
	results := make([]v1alpha1.TrafficTarget, len(t.revisionTargets))
	for i, tt := range t.revisionTargets {
		var pp *int64
		if tt.Percent != nil {
			pp = ptr.Int64(*tt.Percent)
		}

		// We cannot `DeepCopy` here, since tt.TrafficTarget might contain both
		// configuration and revision.
		results[i] = v1alpha1.TrafficTarget{
			TrafficTarget: v1.TrafficTarget{
				Tag:            tt.Tag,
				RevisionName:   tt.RevisionName,
				Percent:        pp,
				LatestRevision: tt.LatestRevision,
			},
		}
		if tt.Tag != "" {
			meta := r.ObjectMeta.DeepCopy()

			hostname, err := domains.HostnameFromTemplate(ctx, meta.Name, tt.Tag)
			if err != nil {
				return nil, err
			}

			labels.SetVisibility(meta, clusterLocalService.Has(hostname))

			// http is currently the only supported scheme
			fullDomain, err := domains.DomainNameFromTemplate(ctx, *meta, hostname)
			if err != nil {
				return nil, err
			}
			results[i].URL = domains.URL(domains.HTTPScheme, fullDomain)
		}
	}
	return results, nil
}

type configBuilder struct {
	configLister listers.ConfigurationLister
	revLister    listers.RevisionLister
	namespace    string

	// targets is a grouping of traffic targets serving the same origin.
	targets map[string]RevisionTargets

	// revisionTargets is the original list of targets, at the Revision level.
	revisionTargets RevisionTargets

	// configurations contains all the referred Configuration, keyed by their name.
	configurations map[string]*v1alpha1.Configuration
	// revisions contains all the referred Revision, keyed by their name.
	revisions map[string]*v1alpha1.Revision

	// TargetError are deferred until we got a complete list of all referred targets.
	deferredTargetErr TargetError
}

func newBuilder(
	configLister listers.ConfigurationLister, revLister listers.RevisionLister,
	namespace string, trafficSize int) *configBuilder {
	return &configBuilder{
		configLister:    configLister,
		revLister:       revLister,
		namespace:       namespace,
		targets:         make(map[string]RevisionTargets),
		revisionTargets: make(RevisionTargets, 0, trafficSize),

		configurations: make(map[string]*v1alpha1.Configuration),
		revisions:      make(map[string]*v1alpha1.Revision),
	}
}

func (t *configBuilder) applySpecTraffic(traffic []v1alpha1.TrafficTarget) error {
	for _, tt := range traffic {
		if err := t.addTrafficTarget(&tt); err != nil {
			// Other non-traffic target errors shouldn't be ignored.
			return err
		}
	}
	return nil
}

func (t *configBuilder) getConfiguration(name string) (*v1alpha1.Configuration, error) {
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

func (t *configBuilder) getRevision(name string) (*v1alpha1.Revision, error) {
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
func (t *configBuilder) deferTargetError(err TargetError) {
	if t.deferredTargetErr == nil || err.IsFailure() {
		t.deferredTargetErr = err
	}
}

func (t *configBuilder) addTrafficTarget(tt *v1alpha1.TrafficTarget) error {
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
func (t *configBuilder) addConfigurationTarget(tt *v1alpha1.TrafficTarget) error {
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
	ntt := tt.TrafficTarget.DeepCopy()
	target := RevisionTarget{
		TrafficTarget: *ntt,
		Active:        !rev.Status.IsActivationRequired(),
		Protocol:      rev.GetProtocol(),
		ServiceName:   rev.Status.ServiceName,
	}
	target.TrafficTarget.RevisionName = rev.Name
	t.addFlattenedTarget(target)
	return nil
}

func (t *configBuilder) addRevisionTarget(tt *v1alpha1.TrafficTarget) error {
	rev, err := t.getRevision(tt.RevisionName)
	if err != nil {
		return err
	}
	if !rev.Status.IsReady() {
		return errUnreadyRevision(rev)
	}
	ntt := tt.TrafficTarget.DeepCopy()
	target := RevisionTarget{
		TrafficTarget: *ntt,
		Active:        !rev.Status.IsActivationRequired(),
		Protocol:      rev.GetProtocol(),
		ServiceName:   rev.Status.ServiceName,
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

func (t *configBuilder) addFlattenedTarget(target RevisionTarget) {
	name := target.TrafficTarget.Tag
	t.revisionTargets = append(t.revisionTargets, target)
	t.targets[DefaultTarget] = append(t.targets[DefaultTarget], target)
	if name != "" {
		t.targets[name] = append(t.targets[name], target)
	}
}

func consolidate(targets RevisionTargets) RevisionTargets {
	byName := make(map[string]RevisionTarget)
	names := []string{}
	for _, tt := range targets {
		name := tt.TrafficTarget.RevisionName
		cur, ok := byName[name]
		if !ok {
			byName[name] = tt
			names = append(names, name)
		} else {
			if tt.TrafficTarget.Percent != nil {
				current := int64(0)
				if cur.TrafficTarget.Percent != nil {
					current += *cur.TrafficTarget.Percent
				}
				current += *tt.TrafficTarget.Percent
				cur.TrafficTarget.Percent = ptr.Int64(current)
			}
			byName[name] = cur
		}
	}
	consolidated := make([]RevisionTarget, len(names))
	for i, name := range names {
		consolidated[i] = byName[name]
	}
	if len(consolidated) == 1 {
		consolidated[0].TrafficTarget.Percent = ptr.Int64(100)
	}
	return consolidated
}

func consolidateAll(targets map[string]RevisionTargets) map[string]RevisionTargets {
	consolidated := make(map[string]RevisionTargets)
	for name, tts := range targets {
		consolidated[name] = consolidate(tts)
	}
	return consolidated
}

func (t *configBuilder) build() (*Config, error) {
	if t.deferredTargetErr != nil {
		t.targets = nil
		t.revisionTargets = nil
	}
	return &Config{
		Targets:         consolidateAll(t.targets),
		revisionTargets: t.revisionTargets,
		Configurations:  t.configurations,
		Revisions:       t.revisions,
	}, t.deferredTargetErr
}
