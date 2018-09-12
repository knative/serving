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

package config

import (
	"context"
	"sync/atomic"

	"github.com/knative/pkg/configmap"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
)

type configsKey struct{}

// +k8s:deepcopy-gen=false
type Config struct {
	Domain *Domain
}

// +k8s:deepcopy-gen=false
type Store struct {
	Logger *zap.SugaredLogger
	domain atomic.Value
}

func FromContext(ctx context.Context) *Config {
	return ctx.Value(configsKey{}).(*Config)
}

func WithConfig(ctx context.Context, c *Config) context.Context {
	return context.WithValue(ctx, configsKey{}, c)
}

func (s *Store) ToContext(ctx context.Context) context.Context {
	return WithConfig(ctx, s.Load())
}

func (s *Store) WatchConfigs(w configmap.Watcher) {
	w.Watch(DomainConfigName, s.setDomain)
}

func (s *Store) Load() *Config {
	return &Config{
		Domain: s.domain.Load().(*Domain).DeepCopy(),
	}
}

func (s *Store) setDomain(c *corev1.ConfigMap) {
	val, err := NewDomainFromConfigMap(c)
	s.save("domain", &s.domain, val, err)
}

func (s *Store) save(desc string, v *atomic.Value, value interface{}, err error) {
	if err != nil {
		if v.Load() != nil {
			s.Logger.Errorf("Error updating route %s config: %v", desc, err)
		} else {
			s.Logger.Fatalf("Error initializing route %s config: %v", desc, err)
		}
		return
	}
	s.Logger.Infof("Route %s config was added or updated: %v", desc, value)
	v.Store(value)
}
