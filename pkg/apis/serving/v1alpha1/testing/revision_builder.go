/*
Copyright 2018 The Knative Authors

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

package testing

import (
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type RevisionBuilder struct {
	obj *v1alpha1.Revision
}

func (bldr RevisionBuilder) Build() *v1alpha1.Revision {
	return bldr.obj.DeepCopy()
}

func (bldr RevisionBuilder) Object() runtime.Object {
	return bldr.Build()
}

func (bldr RevisionBuilder) WithConcurrencyModel(cm v1alpha1.RevisionRequestConcurrencyModelType) RevisionBuilder {
	bldr.obj.Spec.ConcurrencyModel = cm
	return bldr
}

func (bldr RevisionBuilder) WithReadyCondition(status corev1.ConditionStatus) RevisionBuilder {
	bldr.obj.Status.Conditions = []v1alpha1.RevisionCondition{
		{
			Type:   v1alpha1.RevisionConditionReady,
			Status: status,
		},
	}
	return bldr
}

func (bldr RevisionBuilder) WithReadyConditionTrue() RevisionBuilder {
	bldr.obj.Status.Conditions = []v1alpha1.RevisionCondition{
		{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionTrue,
		},
	}
	return bldr
}

func (bldr RevisionBuilder) WithReadyConditionFalse(reason, message string) RevisionBuilder {
	bldr.obj.Status.Conditions = []v1alpha1.RevisionCondition{
		{
			Type:    v1alpha1.RevisionConditionReady,
			Status:  corev1.ConditionFalse,
			Reason:  reason,
			Message: message,
		},
	}
	return bldr
}
