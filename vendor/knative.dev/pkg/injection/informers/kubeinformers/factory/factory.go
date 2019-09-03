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

package factory

import (
	"context"

	"k8s.io/client-go/informers"

	"knative.dev/pkg/controller"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/injection/clients/kubeclient"
	"knative.dev/pkg/logging"
)

func init() {
	injection.Default.RegisterInformerFactory(withInformerFactory)
}

// Key is used as the key for associating information
// with a context.Context.
type Key struct{}

func withInformerFactory(ctx context.Context) context.Context {
	kc := kubeclient.Get(ctx)
	opts := make([]informers.SharedInformerOption, 0, 1)
	if injection.HasNamespaceScope(ctx) {
		opts = append(opts, informers.WithNamespace(injection.GetNamespaceScope(ctx)))
	}
	return context.WithValue(ctx, Key{},
		informers.NewSharedInformerFactoryWithOptions(kc, controller.GetResyncPeriod(ctx), opts...))
}

// Get extracts the Kubernetes InformerFactory from the context.
func Get(ctx context.Context) informers.SharedInformerFactory {
	untyped := ctx.Value(Key{})
	if untyped == nil {
		logging.FromContext(ctx).Panicf(
			"Unable to fetch %T from context.", (informers.SharedInformerFactory)(nil))
	}
	return untyped.(informers.SharedInformerFactory)
}
