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

package clusteringress

import (
	"context"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	ing "github.com/knative/serving/pkg/reconciler/ingress"
)

const (
	ciWorkQueueName = "ClusterIngresses"
)

// NewController works as a constructor for Ingress Controller
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	return ing.CreateController(ctx, cmw, ciWorkQueueName, newInitializer)
}
