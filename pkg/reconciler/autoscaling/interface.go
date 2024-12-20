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

package autoscaling

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
)

// ScalerType classified by Workload type
type ScalerType string

func ScalerTypeFactoryByWR(wr WorkloadResource) ScalerType {
	return ScalerType(fmt.Sprintf("ApiVersion:%s/Kind:%s", wr.ApiVersion, wr.Kind))
}

func ScalerTypeFactory(or v1.ObjectReference) ScalerType {
	return ScalerType(fmt.Sprintf("ApiVersion:%s/Kind:%s", or.APIVersion, or.Kind))
}

type Scaler interface {
	ApplyScale(ctx context.Context, pa *autoscalingv1alpha1.PodAutoscaler, desiredScale int32) error
}

// ScalerFactory the Scaler factory to automatically generate Scaler objects.
type ScalerFactory func(ctx context.Context, bs BaseScaler) (Scaler, error)

// ScalerFactories is a factory holder for any ScalerType
var ScalerFactories = make(map[ScalerType]ScalerFactory)

// ScalerTypes is a scaler instance holder for any ScalerType
var ScalerTypes = make(map[ScalerType]Scaler)

// RegisterScalerType Inject the Scaler factory to automatically generate Scaler objects.
func RegisterScalerType(key ScalerType, factory ScalerFactory) bool {
	if _, ok := ScalerFactories[key]; ok {
		return ok
	} else {
		ScalerFactories[key] = factory
		return ok
	}
}

type BaseScaler interface {
	// GetDynamicClient Get the default dynamic client
	GetDynamicClient() dynamic.Interface

	// GetListerFactory Get the ListerFactory function related to the corresponding workload.
	GetListerFactory() func(schema.GroupVersionResource) (cache.GenericLister, error)
}
