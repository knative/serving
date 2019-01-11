/*
Copyright 2019 The Knative Authors.

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
	"net/http"

	"github.com/knative/pkg/configmap"
)

type cfgKey struct{}

type Config struct {
}

func FromContext(ctx context.Context) *Config {
	return ctx.Value(cfgKey{}).(*Config)
}

func ToContext(ctx context.Context, c *Config) context.Context {
	return context.WithValue(ctx, cfgKey{}, c)
}

// +k8s:deepcopy-gen=false
type Store struct {
	*configmap.UntypedStore
}

// NewStore creates a configuration Store
func NewStore(logger configmap.Logger) *Store {
	return &Store{
		UntypedStore: configmap.NewUntypedStore(
			"activator",
			logger,
			configmap.Constructors{},
		),
	}
}

// ToContext stores the configuration Store in the passed context
func (s *Store) ToContext(ctx context.Context) context.Context {
	return ToContext(ctx, s.Load())
}

// Load creates a Config for this store
func (s *Store) Load() *Config {
	return &Config{}
}

type storeMiddleware struct {
	store *Store
	next  http.Handler
}

// ServeHTTP injects Config in to the context of the http request r
func (mw *storeMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := mw.store.ToContext(r.Context())
	mw.next.ServeHTTP(w, r.WithContext(ctx))
}

// HTTPMiddleware is a middlewhere which stores the current config store in the request context
func (s *Store) HTTPMiddleware(next http.Handler) http.Handler {
	return &storeMiddleware{
		store: s,
		next:  next,
	}
}
