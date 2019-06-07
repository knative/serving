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

// Code generated by injection-gen. DO NOT EDIT.

package fake

import (
	"context"

	controller "github.com/knative/pkg/controller"
	injection "github.com/knative/pkg/injection"
	fake "github.com/knative/serving/pkg/client/injection/informers/networking/factory/fake"
	clusteringress "github.com/knative/serving/pkg/client/injection/informers/networking/v1alpha1/clusteringress"
)

var Get = clusteringress.Get

func init() {
	injection.Fake.RegisterInformer(withInformer)
}

func withInformer(ctx context.Context) (context.Context, controller.Informer) {
	f := fake.Get(ctx)
	inf := f.Networking().V1alpha1().ClusterIngresses()
	return context.WithValue(ctx, clusteringress.Key{}, inf), inf.Informer()
}
