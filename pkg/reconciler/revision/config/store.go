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

package config

import (
	"context"

	"knative.dev/serving/pkg/apis/config"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
	pkgtracing "knative.dev/pkg/tracing/config"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
	"knative.dev/serving/pkg/deployment"
	"knative.dev/serving/pkg/network"
)

type cfgKey struct{}

// +k8s:deepcopy-gen=false
type Config struct {
	Autoscaler    *autoscalerconfig.Config
	Defaults      *config.Defaults
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

// +k8s:deepcopy-gen=false
type Store struct {
	*configmap.UntypedStore
}

// NewStore creates a new store of Configs and optionally calls functions when ConfigMaps are updated for Revisions
func NewStore(logger configmap.Logger, onAfterStore ...func(name string, value interface{})) *Store {
	store := &Store{
		UntypedStore: configmap.NewUntypedStore(
			"revision",
			logger,
			configmap.Constructors{
				autoscalerconfig.ConfigName: autoscalerconfig.NewConfigFromConfigMap,
				config.DefaultsConfigName:   config.NewDefaultsConfigFromConfigMap,
				deployment.ConfigName:       deployment.NewConfigFromConfigMap,
				logging.ConfigMapName():     logging.NewConfigFromConfigMap,
				metrics.ConfigMapName():     metrics.NewObservabilityConfigFromConfigMap,
				network.ConfigName:          network.NewConfigFromConfigMap,
				pkgtracing.ConfigName:       pkgtracing.NewTracingConfigFromConfigMap,
			},
			onAfterStore...,
		),
	}
	return store
}

// ToContext persists the config on the context.
func (s *Store) ToContext(ctx context.Context) context.Context {
	return ToContext(ctx, s.Load())
}

// Load returns the config from the store.
func (s *Store) Load() *Config {
	return &Config{
		Autoscaler:    s.UntypedLoad(autoscalerconfig.ConfigName).(*autoscalerconfig.Config).DeepCopy(),
		Defaults:      s.UntypedLoad(config.DefaultsConfigName).(*config.Defaults).DeepCopy(),
		Deployment:    s.UntypedLoad(deployment.ConfigName).(*deployment.Config).DeepCopy(),
		Logging:       s.UntypedLoad((logging.ConfigMapName())).(*logging.Config).DeepCopy(),
		Network:       s.UntypedLoad(network.ConfigName).(*network.Config).DeepCopy(),
		Observability: s.UntypedLoad(metrics.ConfigMapName()).(*metrics.ObservabilityConfig).DeepCopy(),
		Tracing:       s.UntypedLoad(pkgtracing.ConfigName).(*pkgtracing.Config).DeepCopy(),
	}
}
