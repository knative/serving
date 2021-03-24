/*
Copyright 2019 The Knative Authors

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

	"go.uber.org/atomic"
	"knative.dev/pkg/configmap"
	tracingconfig "knative.dev/pkg/tracing/config"
)

type cfgKey struct{}

// Config is the configuration for the activator.
type Config struct {
	Tracing *tracingconfig.Config
}

// FromContext obtains a Config injected into the passed context.
func FromContext(ctx context.Context) *Config {
	return ctx.Value(cfgKey{}).(*Config)
}

// Store loads/unloads our untyped configuration.
// +k8s:deepcopy-gen=false
type Store struct {
	*configmap.UntypedStore

	// current is the current Config.
	current atomic.Value
}

// NewStore creates a new configuration Store.
func NewStore(logger configmap.Logger, onAfterStore ...func(name string, value interface{})) *Store {
	s := &Store{}

	// Append an update function to run after a ConfigMap has updated to update the
	// current state of the Config.
	onAfterStore = append(onAfterStore, func(_ string, _ interface{}) {
		s.current.Store(&Config{
			Tracing: s.UntypedLoad(tracingconfig.ConfigName).(*tracingconfig.Config).DeepCopy(),
		})
	})
	s.UntypedStore = configmap.NewUntypedStore(
		"activator",
		logger,
		configmap.Constructors{
			tracingconfig.ConfigName: tracingconfig.NewTracingConfigFromConfigMap,
		},
		onAfterStore...,
	)
	return s
}

// ToContext stores the configuration Store in the passed context.
func (s *Store) ToContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, cfgKey{}, s.current.Load())
}
