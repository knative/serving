/*
Copyright 2018 The Knative Authors

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

package traffic

import (
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"

	net "knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	listers "knative.dev/serving/pkg/client/listers/serving/v1"
	"knative.dev/serving/pkg/reconciler/route/domains"
	"knative.dev/serving/pkg/reconciler/route/resources/labels"
)

// DefaultTarget is the unnamed default target for the traffic.
const DefaultTarget = ""

// A RevisionTarget adds the transport protocol and the service name of a
// Revision to a flattened TrafficTarget.
type RevisionTarget struct {
	v1.TrafficTarget
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

	// Visibility of the traffic targets.
	Visibility map[string]netv1alpha1.IngressVisibility

	// A list traffic targets, flattened to the Revision level.  This
	// is used to populate the Route.Status.TrafficTarget field.
	revisionTargets RevisionTargets

	// The referred `Configuration`s and `Revision`s.
	Configurations map[string]*v1.Configuration
	Revisions      map[string]*v1.Revision

	// MissingTargets are references to Configurations or Revisions
	// that are missing
	MissingTargets []corev1.ObjectReference
}

// BuildTrafficConfiguration consolidates and flattens the Route.Spec.Traffic to the Revision-level. It also provides a
// complete lists of Configurations and Revisions referred by the Route, directly or indirectly.  These referred targets
// are keyed by name for easy access.
//
// In the case that some target is missing, an error of type TargetError will be returned.
func BuildTrafficConfiguration(configLister listers.ConfigurationLister, revLister listers.RevisionLister,
	r *v1.Route) (*Config, error) {
	builder := newBuilder(configLister, revLister, r, len(r.Spec.Traffic))
	err := builder.applySpecTraffic(r.Spec.Traffic)
	if err != nil {
		return nil, err
	}
	return builder.build()
}

// GetRevisionTrafficTargets returns a list of TrafficTarget flattened to the RevisionName, and having ConfigurationName cleared out.
func (cfg *Config) GetRevisionTrafficTargets(ctx context.Context, r *v1.Route) ([]v1.TrafficTarget, error) {
	results := make([]v1.TrafficTarget, len(cfg.revisionTargets))
	for i, tt := range cfg.revisionTargets {
		var pp *int64
		if tt.Percent != nil {
			pp = ptr.Int64(*tt.Percent)
		}

		// We cannot `DeepCopy` here, since tt.TrafficTarget might contain both
		// configuration and revision.
		results[i] = v1.TrafficTarget{
			Tag:            tt.Tag,
			RevisionName:   tt.RevisionName,
			Percent:        pp,
			LatestRevision: tt.LatestRevision,
		}
		if tt.Tag != "" {
			meta := r.ObjectMeta.DeepCopy()

			hostname, err := domains.HostnameFromTemplate(ctx, meta.Name, tt.Tag)
			if err != nil {
				return nil, err
			}

			labels.SetVisibility(meta, cfg.Visibility[tt.Tag] == netv1alpha1.IngressVisibilityClusterLocal)

			// HTTP is currently the only supported scheme.
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
	configLister listers.ConfigurationNamespaceLister
	revLister    listers.RevisionNamespaceLister
	route        *v1.Route

	// targets is a grouping of traffic targets serving the same origin.
	targets map[string]RevisionTargets

	// revisionTargets is the original list of targets, at the Revision level.
	revisionTargets RevisionTargets

	// configurations contains all the referred Configuration, keyed by their name.
	configurations map[string]*v1.Configuration
	// revisions contains all the referred Revision, keyed by their name.
	revisions map[string]*v1.Revision

	// missingTargets is a collection of targets that weren't present
	// in our listers
	missingTargets []corev1.ObjectReference

	// TargetError are deferred until we got a complete list of all referred targets.
	deferredTargetErr TargetError
}

func newBuilder(
	configLister listers.ConfigurationLister, revLister listers.RevisionLister,
	r *v1.Route, trafficSize int) *configBuilder {
	return &configBuilder{
		configLister:    configLister.Configurations(r.Namespace),
		revLister:       revLister.Revisions(r.Namespace),
		route:           r,
		targets:         make(map[string]RevisionTargets),
		revisionTargets: make(RevisionTargets, 0, trafficSize),

		configurations: make(map[string]*v1.Configuration),
		revisions:      make(map[string]*v1.Revision),
	}
}

// BuildRollout builds the current rollout state.
// It is expected to be invoked after applySpecTraffic.
// Returned Rollout will be sorted by tag and within tag by configuration
// (only default tag can have more than configuration object attached).
// TODO(vagababov): actually deal with rollouts, vs just report desired state.
func (cfg *Config) BuildRollout() *Rollout {
	rollout := &Rollout{}

	for tag, targets := range cfg.Targets {
		buildRolloutForTag(rollout, tag, targets)
	}
	sortRollout(rollout)
	return rollout
}

// buildRolloutForTag builds the current rollout state.
// It is expected to be invoked after applySpecTraffic.
// TODO(vagababov): actually deal with rollouts, vs just report desired state.
func buildRolloutForTag(r *Rollout, tag string, rts RevisionTargets) {
	// Only main target will have more than 1 element here.
	for _, rt := range rts {
		// Skip if it's revision target.
		if rt.LatestRevision == nil || !*rt.LatestRevision {
			continue
		}

		// The targets with the same revision are already joined together.
		r.Configurations = append(r.Configurations, ConfigurationRollout{
			ConfigurationName: rt.ConfigurationName,
			Tag:               tag,
			Percent:           int(zeroIfNil(rt.Percent)),
			Revisions: []RevisionRollout{{
				RevisionName: rt.RevisionName,
				// Note: this will match config value in steady state, but
				// during rollout it will be overridden by the rollout logic.
				Percent: int(zeroIfNil(rt.Percent)),
			}},
		})
	}
}

func (cb *configBuilder) applySpecTraffic(traffic []v1.TrafficTarget) error {
	for i := range traffic {
		if err := cb.addTrafficTarget(&traffic[i]); err != nil {
			// Other non-traffic target errors shouldn't be ignored.
			return err
		}
	}
	return nil
}

func (cb *configBuilder) getConfiguration(name string) (*v1.Configuration, error) {
	config, ok := cb.configurations[name]
	if !ok {
		var err error
		config, err = cb.configLister.Get(name)
		if apierrs.IsNotFound(err) {
			return nil, errMissingConfiguration(name)
		} else if err != nil {
			return nil, err
		}
		cb.configurations[name] = config
	}
	return config, nil
}

func (cb *configBuilder) getRevision(name string) (*v1.Revision, error) {
	rev, ok := cb.revisions[name]
	if !ok {
		var err error
		rev, err = cb.revLister.Get(name)
		if apierrs.IsNotFound(err) {
			return nil, errMissingRevision(name)
		} else if err != nil {
			return nil, err
		}
		cb.revisions[name] = rev
	}
	return rev, nil
}

// deferTargetError will record a TargetError.  A TargetError with
// IsFailure()=true will always overwrite a previous TargetError.
func (cb *configBuilder) deferTargetError(err TargetError) {
	if cb.deferredTargetErr == nil || err.IsFailure() {
		cb.deferredTargetErr = err
	}
}

func (cb *configBuilder) addTrafficTarget(tt *v1.TrafficTarget) error {
	var err error
	if tt.RevisionName != "" {
		err = cb.addRevisionTarget(tt)
	} else if tt.ConfigurationName != "" {
		err = cb.addConfigurationTarget(tt)
	}
	if err != nil {
		var errMissingTarget *missingTargetError
		if errors.As(err, &errMissingTarget) {
			apiVersion, kind := v1.SchemeGroupVersion.
				WithKind(errMissingTarget.kind).
				ToAPIVersionAndKind()

			cb.missingTargets = append(cb.missingTargets, corev1.ObjectReference{
				APIVersion: apiVersion,
				Kind:       kind,
				Name:       errMissingTarget.name,
				Namespace:  cb.route.Namespace,
			})
		}

		var errTarget TargetError
		if errors.As(err, &errTarget) {
			// Defer target errors, as we still want to compile a list of
			// all referred targets, including missing ones.
			cb.deferTargetError(errTarget)
			return nil
		}
	}
	return err
}

// addConfigurationTarget flattens a traffic target to the Revision level, by looking up for the LatestReadyRevisionName
// on the referred Configuration.  It adds both to the lists of directly referred targets.
func (cb *configBuilder) addConfigurationTarget(tt *v1.TrafficTarget) error {
	config, err := cb.getConfiguration(tt.ConfigurationName)
	if err != nil {
		return err
	}
	if config.Status.LatestReadyRevisionName == "" {
		return errUnreadyConfiguration(config)
	}
	rev, err := cb.getRevision(config.Status.LatestReadyRevisionName)
	if err != nil {
		return err
	}
	ntt := tt.DeepCopy()
	target := RevisionTarget{
		TrafficTarget: *ntt,
		Protocol:      rev.GetProtocol(),
		ServiceName:   rev.Status.ServiceName,
	}
	target.TrafficTarget.RevisionName = rev.Name
	cb.addFlattenedTarget(target)
	return nil
}

func (cb *configBuilder) addRevisionTarget(tt *v1.TrafficTarget) error {
	rev, err := cb.getRevision(tt.RevisionName)
	if err != nil {
		return err
	}
	if !rev.IsReady() {
		return errUnreadyRevision(rev)
	}
	ntt := tt.DeepCopy()
	target := RevisionTarget{
		TrafficTarget: *ntt,
		Protocol:      rev.GetProtocol(),
		ServiceName:   rev.Status.ServiceName,
	}
	if configName, ok := rev.Labels[serving.ConfigurationLabelKey]; ok {
		target.TrafficTarget.ConfigurationName = configName
		if _, err := cb.getConfiguration(configName); err != nil {
			return err
		}
	}
	cb.addFlattenedTarget(target)
	return nil
}

// zeroIfNil returns `0` if `ptr==nil`, or `*ptr` otherwise.
func zeroIfNil(ptr *int64) int64 {
	if ptr == nil {
		return 0
	}
	return *ptr
}

// This find the exact revision+tag pair and if so, just adds the percentages.
// This expects single digit lists, so just does an O(N) search.
func mergeIfNecessary(rts RevisionTargets, rt RevisionTarget) RevisionTargets {
	for i := range rts {
		if rts[i].Tag == rt.Tag && rts[i].RevisionName == rt.RevisionName &&
			*rt.LatestRevision == *rts[i].LatestRevision {
			rts[i].Percent = ptr.Int64(zeroIfNil(rts[i].Percent) + zeroIfNil(rt.Percent))
			return rts
		}
	}
	return append(rts, rt)
}

func (cb *configBuilder) addFlattenedTarget(target RevisionTarget) {
	name := target.TrafficTarget.Tag
	cb.revisionTargets = mergeIfNecessary(cb.revisionTargets, target)
	cb.targets[DefaultTarget] = append(cb.targets[DefaultTarget], target)
	if name != "" {
		// This should always have just a single entry at most.
		cb.targets[name] = append(cb.targets[name], target)
	}
}

func (cb *configBuilder) build() (*Config, error) {
	if cb.deferredTargetErr != nil {
		cb.targets = nil
		cb.revisionTargets = nil
	}
	return &Config{
		Targets:         consolidateAll(cb.targets),
		revisionTargets: cb.revisionTargets,
		Configurations:  cb.configurations,
		Revisions:       cb.revisions,
		MissingTargets:  cb.missingTargets,
	}, cb.deferredTargetErr
}

func consolidateAll(targets map[string]RevisionTargets) map[string]RevisionTargets {
	consolidated := make(map[string]RevisionTargets, len(targets))
	for name, tts := range targets {
		consolidated[name] = consolidate(tts)
	}
	return consolidated
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
			continue
		}
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
	consolidated := make([]RevisionTarget, len(names))
	for i, name := range names {
		consolidated[i] = byName[name]
	}
	if len(consolidated) == 1 {
		consolidated[0].TrafficTarget.Percent = ptr.Int64(100)
	}
	return consolidated
}
