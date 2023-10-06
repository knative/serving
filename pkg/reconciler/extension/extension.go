/*
Copyright 2022 The Knative Authors

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

package extension

import (
	"context"

	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

// Extension enables pluggable extensive features added into the controller
type Extension interface {
	TransformRevision(*v1.Revision) *v1.Revision
	PostRevisionReconcile(context.Context, *autoscalingv1alpha1.PodAutoscaler, *autoscalingv1alpha1.PodAutoscaler) error
	PostConfigurationReconcile(context.Context, *v1.Service, *v1.Configuration) error
	TransformService(*v1.Service) *v1.Service
	UpdateExtensionStatus(context.Context, *v1.Service) (bool, error)
}

func NoExtension() Extension {
	return &nilExtension{}
}

type nilExtension struct {
}

func (nilExtension) TransformRevision(revision *v1.Revision) *v1.Revision {
	return revision
}

func (nilExtension) PostRevisionReconcile(context.Context, *autoscalingv1alpha1.PodAutoscaler, *autoscalingv1alpha1.PodAutoscaler) error {
	return nil
}

func (nilExtension) PostConfigurationReconcile(context.Context, *v1.Service, *v1.Configuration) error {
	return nil
}

func (nilExtension) TransformService(service *v1.Service) *v1.Service {
	return service
}

func (nilExtension) UpdateExtensionStatus(context.Context, *v1.Service) (bool, error) {
	return true, nil
}
