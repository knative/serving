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

package workload

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/apis/duck"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/reconciler/autoscaling"
	"knative.dev/serving/pkg/resources"
)

type DeploymentScaler struct {
	ctx context.Context
	bs  autoscaling.BaseScaler
}

var _ autoscaling.Scaler = &DeploymentScaler{}

func init() {
	autoscaling.RegisterScalerType(autoscaling.ScalerTypeFactoryByWR(autoscaling.DEPLOYMENT), NewDeploymentScaler)
}

func NewDeploymentScaler(ctx context.Context, bs autoscaling.BaseScaler) (autoscaling.Scaler, error) {
	return &DeploymentScaler{ctx: ctx, bs: bs}, nil
}

func (a *DeploymentScaler) ApplyScale(ctx context.Context, pa *autoscalingv1alpha1.PodAutoscaler, desiredScale int32) error {
	ps, err := resources.GetScaleResource(pa.Namespace, pa.Spec.ScaleTargetRef, a.bs.GetListerFactory())
	if err != nil {
		return fmt.Errorf("failed to get scale target %v: %w", pa.Spec.ScaleTargetRef, err)
	}
	gvr, _, err := resources.ScaleResourceArguments(pa.Spec.ScaleTargetRef)
	if err != nil {
		return err
	}
	psNew := ps.DeepCopy()
	psNew.Spec.Replicas = &desiredScale
	patch, err := duck.CreatePatch(ps, psNew)
	if err != nil {
		return err
	}
	patchBytes, err := patch.MarshalJSON()
	if err != nil {
		return err
	}

	_, err = a.bs.GetDynamicClient().Resource(*gvr).Namespace(pa.Namespace).Patch(ctx, ps.Name, types.JSONPatchType,
		patchBytes, metav1.PatchOptions{})
	return err
}
