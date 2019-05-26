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

	informers "github.com/knative/pkg/client/informers/externalversions"

	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/injection"
	"github.com/knative/serving/pkg/injection/clients/sharedclient"
)

func init() {
	injection.Default.RegisterInformerFactory(withSharedInformerFactory)
}

// Key is used as the key for associating information
// with a context.Context.
type Key struct{}

func withSharedInformerFactory(ctx context.Context) context.Context {
	sc := sharedclient.Get(ctx)
	return context.WithValue(ctx, Key{},
		informers.NewSharedInformerFactory(sc, controller.GetResyncPeriod(ctx)))
}

// Get extracts the Shared InformerFactory from the context.
func Get(ctx context.Context) informers.SharedInformerFactory {
	return ctx.Value(Key{}).(informers.SharedInformerFactory)
}
