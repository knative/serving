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

	netcfg "knative.dev/networking/pkg/config"
	"knative.dev/pkg/configmap"
	asconfig "knative.dev/serving/pkg/autoscaler/config"
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"
	"knative.dev/serving/pkg/deployment"
)

type cfgKey struct{}

// Config of the Autoscaler.
type Config struct {
	Autoscaler *autoscalerconfig.Config
	Deployment *deployment.Config
	Network    *netcfg.Config
}

// FromContext fetch config from context.
func FromContext(ctx context.Context) *Config {
	return ctx.Value(cfgKey{}).(*Config)
}

// ToContext adds config to given context.
func ToContext(ctx context.Context, c *Config) context.Context {
	return context.WithValue(ctx, cfgKey{}, c)
}

// Store is configmap.UntypedStore based config store.
type Store struct {
	*configmap.UntypedStore
}

// NewStore creates a configmap.UntypedStore based config store.
//
// logger must be non-nil implementation of configmap.Logger (commonly used
// loggers conform)
//
// onAfterStore is a variadic list of callbacks to run
// after the ConfigMap has been processed and stored.
//
// See also: configmap.NewUntypedStore().
func NewStore(logger configmap.Logger, onAfterStore ...func(name string, value interface{})) *Store {
	store := &Store{
		UntypedStore: configmap.NewUntypedStore(
			"autoscaler",
			logger,
			configmap.Constructors{
				asconfig.ConfigName:   asconfig.NewConfigFromConfigMap,
				deployment.ConfigName: deployment.NewConfigFromConfigMap,
				netcfg.ConfigMapName:  netcfg.NewConfigFromConfigMap,
			},
			onAfterStore...,
		),
	}
	return store
}

// ToContext adds Store contents to given context.
func (s *Store) ToContext(ctx context.Context) context.Context {
	return ToContext(ctx, s.Load())
}

// Load fetches config from Store.
func (s *Store) Load() *Config {
	return &Config{
		Autoscaler: s.UntypedLoad(asconfig.ConfigName).(*autoscalerconfig.Config).DeepCopy(),
		Deployment: s.UntypedLoad(deployment.ConfigName).(*deployment.Config).DeepCopy(),
		Network:    s.UntypedLoad(netcfg.ConfigMapName).(*netcfg.Config).DeepCopy(),
	}
}
