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

package cachingclient

import (
	"context"

	cachingclientset "github.com/knative/caching/pkg/client/clientset/versioned"
	"k8s.io/client-go/rest"

	"github.com/knative/pkg/injection"
)

func init() {
	injection.Default.RegisterClient(withCachingClient)
}

// Key is used as the key for associating information
// with a context.Context.
type Key struct{}

func withCachingClient(ctx context.Context, cfg *rest.Config) context.Context {
	return context.WithValue(ctx, Key{}, cachingclientset.NewForConfigOrDie(cfg))
}

// Get extracts the caching client from the context.
func Get(ctx context.Context) cachingclientset.Interface {
	return ctx.Value(Key{}).(cachingclientset.Interface)
}
