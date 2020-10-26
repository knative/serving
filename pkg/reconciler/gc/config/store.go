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

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/logging"
	apiconfig "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/gc"
)

type cfgKey struct{}

// Config is the configuration for GC.
type Config struct {
	RevisionGC *gc.Config
	Features   *apiconfig.Features
}

// FromContext fetches the config from the context.
func FromContext(ctx context.Context) *Config {
	return ctx.Value(cfgKey{}).(*Config)
}

// ToContext adds config to the given context.
func ToContext(ctx context.Context, c *Config) context.Context {
	return context.WithValue(ctx, cfgKey{}, c)
}

// Store is a configmap.UntypedStore based config store.
type Store struct {
	*configmap.UntypedStore
}

// ToContext adds config to given context.
func (s *Store) ToContext(ctx context.Context) context.Context {
	return ToContext(ctx, s.Load())
}

// Load fetches config from Store.
func (s *Store) Load() *Config {
	return &Config{
		RevisionGC: s.UntypedLoad(gc.ConfigName).(*gc.Config).DeepCopy(),
		Features:   s.UntypedLoad(apiconfig.FeaturesConfigName).(*apiconfig.Features).DeepCopy(),
	}
}

// NewStore creates a configmap.UntypedStore based config store.
func NewStore(ctx context.Context, onAfterStore ...func(name string, value interface{})) *Store {
	return &Store{
		UntypedStore: configmap.NewUntypedStore(
			"configuration",
			logging.FromContext(ctx),
			configmap.Constructors{
				gc.ConfigName:                gc.NewConfigFromConfigMapFunc(ctx),
				apiconfig.FeaturesConfigName: apiconfig.NewFeaturesConfigFromConfigMap,
			},
			onAfterStore...,
		),
	}
}
