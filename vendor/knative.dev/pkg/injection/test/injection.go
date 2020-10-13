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

package test

import (
	"context"
	"knative.dev/pkg/signals"
	"sync"

	"knative.dev/pkg/injection"
)

var (
	injectionContext context.Context
	icOnce           sync.Once
)

// InjectionContext returns the context for tests leveraging client injection.
func InjectionContext() context.Context {
	icOnce.Do(func() {
		cfg := injection.ParseAndGetRESTConfigOrDie()

		// Normal e2e tests poll, so set limits high.
		cfg.QPS = 100
		cfg.Burst = 200

		var startInformers func()
		injectionContext, startInformers = injection.EnableInjectionOrDie(signals.NewContext(), cfg)
		startInformers()
	})
	return injectionContext
}
