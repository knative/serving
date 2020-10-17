/*
Copyright 2020 The Knative Authors

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
)

type cfgKey struct{}

// Config holds the collection of configurations that we attach to contexts.
type Config struct {
	Network *network.Config
}

// FromContext extracts a Config from the provided context.
func FromContext(ctx context.Context) *Config {
	return ctx.Value(cfgKey{}).(*Config)
}

// ToContext attaches the provided Config to the provided context, returning the
// new context with the Config attached.
func ToContext(ctx context.Context, c *Config) context.Context {
	return context.WithValue(ctx, cfgKey{}, c)
}

// Store is a typed wrapper around configmap.Untyped store to handle our configmaps.
type Store struct {
	*configmap.UntypedStore
}

// ToContext attaches the current Config state to the provided context.
func (s *Store) ToContext(ctx context.Context) context.Context {
	return ToContext(ctx, s.Load())
}

// Load creates a Config from the current config state of the Store.
func (s *Store) Load() *Config {
	return &Config{
		Network: s.UntypedLoad(network.ConfigName).(*network.Config).DeepCopy(),
	}
}

// NewStore creates a new store of Configs and optionally calls functions when ConfigMaps are updated.
func NewStore(ctx context.Context, onAfterStore ...func(name string, value interface{})) *Store {
	return &Store{
		UntypedStore: configmap.NewUntypedStore(
			"domainmapping",
			logging.FromContext(ctx),
			configmap.Constructors{
				network.ConfigName: network.NewConfigFromConfigMap,
			},
			onAfterStore...,
		),
	}
}
