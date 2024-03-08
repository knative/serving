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

package injection

import (
	"context"

	"go.uber.org/zap"
	"k8s.io/client-go/rest"

	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/signals"
)

// EnableInjectionOrDie enables Knative Client Injection, and provides a
// callback to start the informers. Both Context and Config are optional.
// Returns context with rest config set and a callback to start the informers
// after watches have been set.
//
// Typical integration:
// ```go
//
//	ctx, startInformers := injection.EnableInjectionOrDie(signals.NewContext(), nil)
//	... start watches with informers, if required ...
//	startInformers()
//
// ```
func EnableInjectionOrDie(ctx context.Context, cfg *rest.Config) (context.Context, func()) {
	if ctx == nil {
		ctx = signals.NewContext()
	}
	if cfg == nil {
		cfg = ParseAndGetRESTConfigOrDie()
	}

	// Respect user provided settings, but if omitted customize the default behavior.
	if cfg.QPS == 0 {
		cfg.QPS = rest.DefaultQPS
	}
	if cfg.Burst == 0 {
		cfg.Burst = rest.DefaultBurst
	}
	ctx = WithConfig(ctx, cfg)

	ctx, informers := Default.SetupInformers(ctx, cfg)

	includedInformers := make([]controller.Informer, 0)
	
	for _, inf := range informers {
		if p := getInformerExcludingFilter(ctx); p != nil {
			skip, name := (*p)(ctx, inf)
			if skip {
				logging.FromContext(ctx).Infof("Excluding informer %s", name)
				continue
			}
		}
		includedInformers = append(includedInformers, inf)
	}

	return ctx, func() {
		logging.FromContext(ctx).Infof("Starting %d informers...", len(includedInformers))
		if err := controller.StartInformers(ctx.Done(), includedInformers...); err != nil {
			logging.FromContext(ctx).Fatalw("Failed to start informers", zap.Error(err))
		}
	}
}

type InformerExcludingFilter func(ctx context.Context, inf controller.Informer) (bool, string)
type excludeInformerPredicateKey struct{}

// WithInformerExcludingFilter sets the predicate to exclude informers from being started.
// If predicate returns true the informer will not be started.
func WithInformerExcludingFilter(ctx context.Context, predicate InformerExcludingFilter) context.Context {
	return context.WithValue(ctx, excludeInformerPredicateKey{}, predicate)
}

func getInformerExcludingFilter(ctx context.Context) *InformerExcludingFilter {
	untyped := ctx.Value(excludeInformerPredicateKey{})
	if untyped == nil {
		return nil
	}
	e := untyped.(InformerExcludingFilter)
	return &e
}
