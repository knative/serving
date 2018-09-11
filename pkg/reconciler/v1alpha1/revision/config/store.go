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
	"sync/atomic"

	"github.com/knative/pkg/configmap"
	pkglogging "github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/logging"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
)

type configsKey struct{}

// +k8s:deepcopy-gen=false
type Config struct {
	Controller    *Controller
	Network       *Network
	Observability *Observability
	Logging       *pkglogging.Config
	Autoscaler    *autoscaler.Config
}

// +k8s:deepcopy-gen=false
type Store struct {
	Logger *zap.SugaredLogger

	controller    atomic.Value
	network       atomic.Value
	observability atomic.Value
	logging       atomic.Value
	autoscaler    atomic.Value
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
	w.Watch(NetworkConfigName, s.setNetwork)
	w.Watch(ObservabilityConfigName, s.setObservability)
	w.Watch(ControllerConfigName, s.setController)

	w.Watch(autoscaler.ConfigName, s.setAutoscaler)
	w.Watch(logging.ConfigName, s.setLogging)
}

func (s *Store) Load() *Config {
	return &Config{
		Controller:    s.controller.Load().(*Controller).DeepCopy(),
		Network:       s.network.Load().(*Network).DeepCopy(),
		Observability: s.observability.Load().(*Observability).DeepCopy(),
		Logging:       s.logging.Load().(*pkglogging.Config).DeepCopy(),
		Autoscaler:    s.autoscaler.Load().(*autoscaler.Config).DeepCopy(),
	}
}

func (s *Store) setController(c *corev1.ConfigMap) {
	val, err := NewControllerConfigFromConfigMap(c)
	s.save("controller", &s.controller, val, err)
}

func (s *Store) setNetwork(c *corev1.ConfigMap) {
	val, err := NewNetworkFromConfigMap(c)
	s.save("network", &s.network, val, err)
}

func (s *Store) setObservability(c *corev1.ConfigMap) {
	val, err := NewObservabilityFromConfigMap(c)
	s.save("observability", &s.observability, val, err)
}

func (s *Store) setLogging(c *corev1.ConfigMap) {
	val, err := logging.NewConfigFromConfigMap(c)
	s.save("logging", &s.logging, val, err)
}

func (s *Store) setAutoscaler(c *corev1.ConfigMap) {
	val, err := autoscaler.NewConfigFromConfigMap(c)
	s.save("autoscaler", &s.autoscaler, val, err)
}

func (s *Store) save(desc string, v *atomic.Value, value interface{}, err error) {
	if err != nil {
		if v.Load() != nil {
			s.Logger.Errorf("Error updating revision %s config: %v", desc, err)
		} else {
			s.Logger.Fatalf("Error initializing revision %s config: %v", desc, err)
		}
		return
	}
	s.Logger.Infof("Revision %s config was added or updated: %v", desc, value)
	v.Store(value)
}
