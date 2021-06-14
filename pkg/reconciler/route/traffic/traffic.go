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
	"knative.dev/pkg/apis"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	listers "knative.dev/serving/pkg/client/listers/serving/v1"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/domains"
	"knative.dev/serving/pkg/reconciler/route/resources/labels"
)

// DefaultTarget is the unnamed default target for the traffic.
const DefaultTarget = ""

// A RevisionTarget adds the transport protocol and the service name of a
// Revision to a flattened TrafficTarget.
type RevisionTarget struct {
	v1.TrafficTarget
	Protocol net.ProtocolType
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
	return newBuilder(configLister, revLister, r).build()
}

func rolloutConfig(cfgName string, ros []*ConfigurationRollout) *ConfigurationRollout {
	for _, ro := range ros {
		if ro.ConfigurationName == cfgName {
			return ro
		}
	}
	// Technically impossible with valid inputs.
	return nil
}

func (cfg *Config) computeURL(ctx context.Context, r *v1.Route, tt *RevisionTarget) (*apis.URL, error) {
	meta := r.ObjectMeta.DeepCopy()

	hostname, err := domains.HostnameFromTemplate(ctx, meta.Name, tt.Tag)
	if err != nil {
		return nil, err
	}

	isClusterLocal := cfg.Visibility[tt.Tag] == netv1alpha1.IngressVisibilityClusterLocal
	labels.SetVisibility(meta, isClusterLocal)

	fullDomain, err := domains.DomainNameFromTemplate(ctx, *meta, hostname)
	if err != nil {
		return nil, err
	}

	scheme := "http"
	if !isClusterLocal {
		scheme = config.FromContext(ctx).Network.DefaultExternalScheme
	}

	return domains.URL(scheme, fullDomain), nil
}

func (cfg *Config) targetToStatus(ctx context.Context, r *v1.Route, tt *RevisionTarget,
	revs []RevisionRollout, results []v1.TrafficTarget) (_ []v1.TrafficTarget, err error) {
	var url *apis.URL
	// Do this once per tag.
	if tt.Tag != "" {
		url, err = cfg.computeURL(ctx, r, tt)
		if err != nil {
			return nil, err
		}
	}

	for i := range revs {
		rr := &revs[i]
		// We cannot `DeepCopy` here, since tt.TrafficTarget might contain both
		// configuration and revision.
		result := v1.TrafficTarget{
			Tag:            tt.Tag,
			RevisionName:   rr.RevisionName,
			Percent:        ptr.Int64(int64(rr.Percent)),
			LatestRevision: tt.LatestRevision,
		}

		if tt.Tag != "" {
			result.URL = url
		}
		results = append(results, result)
	}
	return results, nil
}

// GetRevisionTrafficTargets returns a list of TrafficTarget flattened to the RevisionName, and having ConfigurationName cleared out.
func (cfg *Config) GetRevisionTrafficTargets(ctx context.Context, r *v1.Route, ro *Rollout) ([]v1.TrafficTarget, error) {
	results := make([]v1.TrafficTarget, 0, len(cfg.revisionTargets))
	for i := range cfg.revisionTargets {
		tt := &cfg.revisionTargets[i]
		var (
			roCfg *ConfigurationRollout
			revs  []RevisionRollout
			err   error
		)

		if tt.LatestRevision != nil && *tt.LatestRevision {
			cfgs := ro.RolloutsByTag(tt.Tag)
			roCfg = rolloutConfig(tt.ConfigurationName, cfgs)
		}

		// Not a rollout, or just a revision target. Setup mock revision rollout
		// with a single element.
		if roCfg == nil {
			revs = []RevisionRollout{{
				RevisionName: tt.RevisionName,
				Percent:      int(*tt.Percent),
			}}
		} else {
			revs = roCfg.Revisions
		}
		results, err = cfg.targetToStatus(ctx, r, tt, revs, results)
		if err != nil {
			return nil, err
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
	r *v1.Route) *configBuilder {
	return &configBuilder{
		configLister:    configLister.Configurations(r.Namespace),
		revLister:       revLister.Revisions(r.Namespace),
		route:           r,
		targets:         make(map[string]RevisionTargets, 1),
		revisionTargets: make(RevisionTargets, 0, len(r.Spec.Traffic)),

		configurations: make(map[string]*v1.Configuration),
		revisions:      make(map[string]*v1.Revision, 1),
	}
}

// BuildRollout builds the current rollout state.
// It is expected to be invoked after applySpecTraffic.
// Returned Rollout will be sorted by tag and within tag by configuration
// (only default tag can have more than configuration object attached).
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
func buildRolloutForTag(r *Rollout, tag string, rts RevisionTargets) {
	// Only main target will have more than 1 element here.
	for _, rt := range rts {
		// Skip if it's revision target.
		if rt.LatestRevision == nil || !*rt.LatestRevision {
			continue
		}

		// Ignore the rollouts with 0 percent target traffic.
		// This can happen only for the default tag.
		if *rt.Percent == 0 {
			continue
		}
		// The targets with the same revision are already joined together.
		r.Configurations = append(r.Configurations, &ConfigurationRollout{
			ConfigurationName: rt.ConfigurationName,
			Tag:               tag,
			Percent:           int(*rt.Percent),
			Revisions: []RevisionRollout{{
				RevisionName: rt.RevisionName,
				// Note: this will match config value in steady state, but
				// during rollout it will be overridden by the rollout logic.
				Percent: int(*rt.Percent),
			}},
		})
	}
}

func (cb *configBuilder) applySpecTraffic() error {
	traffic := cb.route.Spec.Traffic
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
		}
		if err != nil {
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

func (cb *configBuilder) addTrafficTarget(tt *v1.TrafficTarget) (err error) {
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

// This find the exact revision+tag pair and if so, just adds the percentages.
// This expects single digit lists, so just does an O(N) search.
func mergeIfNecessary(rts RevisionTargets, rt RevisionTarget) RevisionTargets {
	for i := range rts {
		if rts[i].Tag == rt.Tag && rts[i].RevisionName == rt.RevisionName &&
			*rt.LatestRevision == *rts[i].LatestRevision {
			rts[i].Percent = ptr.Int64(*rts[i].Percent + *rt.Percent)
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
	if err := cb.applySpecTraffic(); err != nil {
		return nil, err
	}
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
	var names []string
	for _, tt := range targets {
		name := tt.TrafficTarget.RevisionName
		cur, ok := byName[name]
		if !ok {
			byName[name] = tt
			names = append(names, name)
			continue
		}
		cur.TrafficTarget.Percent = ptr.Int64(
			*cur.TrafficTarget.Percent + *tt.TrafficTarget.Percent)
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
