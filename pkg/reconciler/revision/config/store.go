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

package config

import (
	"context"

	network "knative.dev/networking/pkg"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
	pkgtracing "knative.dev/pkg/tracing/config"
	apiconfig "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/deployment"
)

type cfgKey struct{}

// Config contains the configmaps requires for revision reconciliation.
type Config struct {
	*apiconfig.Config
	Deployment    *deployment.Config
	Logging       *logging.Config
	Network       *network.Config
	Observability *metrics.ObservabilityConfig
	Tracing       *pkgtracing.Config
}

// FromContext loads the configuration from the context.
func FromContext(ctx context.Context) *Config {
	return ctx.Value(cfgKey{}).(*Config)
}

// ToContext persists the configuration to the context.
func ToContext(ctx context.Context, c *Config) context.Context {
	return context.WithValue(ctx, cfgKey{}, c)
}

// Store is a typed wrapper around configmap.UntypedStore to handle our configmaps.
type Store struct {
	*configmap.UntypedStore
	apiStore *apiconfig.Store
}

// NewStore creates a new store of Configs and optionally calls functions when ConfigMaps are updated for Revisions
func NewStore(logger configmap.Logger, onAfterStore ...func(name string, value interface{})) *Store {
	store := &Store{
		UntypedStore: configmap.NewUntypedStore(
			"revision",
			logger,
			configmap.Constructors{
				deployment.ConfigName:   deployment.NewConfigFromConfigMap,
				logging.ConfigMapName(): logging.NewConfigFromConfigMap,
				metrics.ConfigMapName(): metrics.NewObservabilityConfigFromConfigMap,
				network.ConfigName:      network.NewConfigFromConfigMap,
				pkgtracing.ConfigName:   pkgtracing.NewTracingConfigFromConfigMap,
			},
			onAfterStore...,
		),
		apiStore: apiconfig.NewStore(logger),
	}
	return store
}

// WatchConfigs uses the provided configmap.Watcher
// to setup watches for the config names provided in the
// Constructors map
func (s *Store) WatchConfigs(cmw configmap.Watcher) {
	s.UntypedStore.WatchConfigs(cmw)
	s.apiStore.WatchConfigs(cmw)
}

// ToContext persists the config on the context.
func (s *Store) ToContext(ctx context.Context) context.Context {
	return s.apiStore.ToContext(ToContext(ctx, s.Load()))
}

// Load returns the config from the store.
func (s *Store) Load() *Config {
	cfg := &Config{
		Config: s.apiStore.Load(),
	}

	if dep, ok := s.UntypedLoad(deployment.ConfigName).(*deployment.Config); ok {
		cfg.Deployment = dep.DeepCopy()
	}
	if log, ok := s.UntypedLoad((logging.ConfigMapName())).(*logging.Config); ok {
		cfg.Logging = log.DeepCopy()
	}
	if net, ok := s.UntypedLoad(network.ConfigName).(*network.Config); ok {
		cfg.Network = net.DeepCopy()
	}
	if obs, ok := s.UntypedLoad(metrics.ConfigMapName()).(*metrics.ObservabilityConfig); ok {
		cfg.Observability = obs.DeepCopy()
	}
	if tr, ok := s.UntypedLoad(pkgtracing.ConfigName).(*pkgtracing.Config); ok {
		cfg.Tracing = tr.DeepCopy()
	}

	return cfg
}
