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
	"strconv"
	"time"

	"github.com/knative/pkg/configmap"
	corev1 "k8s.io/api/core/v1"
)

const (
	RevisionGCConfigName = "config-gc"
)

type cfgKey struct{}

// +k8s:deepcopy-gen=false
type Config struct {
	RevisionGC *RevisionGC
}

func FromContext(ctx context.Context) *Config {
	return ctx.Value(cfgKey{}).(*Config)
}

func ToContext(ctx context.Context, c *Config) context.Context {
	return context.WithValue(ctx, cfgKey{}, c)
}

type RevisionGC struct {
	// Delay duration after a revision create before considering it for GC
	StaleRevisionCreateDelay time.Duration
	// Timeout since a revision lastPinned before it should be GC'd
	StaleRevisionTimeout time.Duration
	// Minimum number of generations of revisions to keep before considering for GC
	StaleRevisionMinimumGenerations int64
}

// +k8s:deepcopy-gen=false
type Store struct {
	*configmap.UntypedStore
}

func (s *Store) ToContext(ctx context.Context) context.Context {
	return ToContext(ctx, s.Load())
}

func (s *Store) Load() *Config {
	return &Config{
		RevisionGC: s.UntypedLoad(RevisionGCConfigName).(*RevisionGC).DeepCopy(),
	}
}

func NewStore(logger configmap.Logger) *Store {
	return &Store{
		UntypedStore: configmap.NewUntypedStore(
			"configuration",
			logger,
			configmap.Constructors{
				RevisionGCConfigName: NewRevisionGCFromConfigMap,
			},
		),
	}
}

func NewRevisionGCFromConfigMap(configMap *corev1.ConfigMap) (*RevisionGC, error) {
	c := RevisionGC{}

	for _, dur := range []struct {
		key          string
		field        *time.Duration
		defaultValue time.Duration
	}{{
		key:          "stale-revision-create-delay",
		field:        &c.StaleRevisionCreateDelay,
		defaultValue: 5 * time.Minute,
	}, {
		key:          "stale-revision-timeout",
		field:        &c.StaleRevisionTimeout,
		defaultValue: 5 * time.Minute,
	}} {
		if raw, ok := configMap.Data[dur.key]; !ok {
			*dur.field = dur.defaultValue
		} else if val, err := time.ParseDuration(raw); err != nil {
			return nil, err
		} else {
			*dur.field = val
		}
	}

	if raw, ok := configMap.Data["stale-revision-minimum-generations"]; !ok {
		c.StaleRevisionMinimumGenerations = 1
	} else if val, err := strconv.ParseInt(raw, 10, 64); err != nil {
		return nil, err
	} else {
		c.StaleRevisionMinimumGenerations = val
	}

	return &c, nil
}
