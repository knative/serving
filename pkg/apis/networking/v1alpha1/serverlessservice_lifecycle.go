/*
Copyright 2019 The Knative Authors.

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

package v1alpha1

import (
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var serverlessServiceCondSet = duckv1alpha1.NewLivingConditionSet(
	ServerlessServiceConditionEndspointsPopulated,
)

// GetGroupVersionKind returns the GVK for the ServerlessService.
func (ci *ServerlessService) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("ServerlessService")
}

// GetCondition returns the value of the condition `t`.
func (cis *ServerlessServiceStatus) GetCondition(t duckv1alpha1.ConditionType) *duckv1alpha1.Condition {
	return serverlessServiceCondSet.Manage(cis).GetCondition(t)
}

// InitializeConditions initializes the conditions.
func (cis *ServerlessServiceStatus) InitializeConditions() {
	serverlessServiceCondSet.Manage(cis).InitializeConditions()
}

// MarkEndpointsPopulated marks the ServerlessServiceStatus endpoints populated condition to true.
func (cis *ServerlessServiceStatus) MarkEndpointsPopulated() {
	serverlessServiceCondSet.Manage(cis).MarkTrue(ServerlessServiceConditionEndspointsPopulated)
}

// IsReady looks at the conditions and if the Status has a condition
// ServerlessServiceConditionReady returns true if ConditionStatus is True.
func (cis *ServerlessServiceStatus) IsReady() bool {
	return serverlessServiceCondSet.Manage(cis).IsHappy()
}
