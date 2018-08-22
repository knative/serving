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
	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"
)

type ConfigToBuildFunc func(*v1alpha1.Configuration) *buildv1alpha1.Build
type ConfigToRevisionFunc func(*v1alpha1.Configuration) *v1alpha1.Revision

type ConfigBuilder struct {
	obj *v1alpha1.Configuration
}

func Config(name, namespace string) ConfigBuilder {
	return ConfigBuilder{&v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.ConfigurationSpec{
			RevisionTemplate: v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					Container: corev1.Container{
						Image: "busybox",
					},
				},
			},
		},
		Status: v1alpha1.ConfigurationStatus{
			Conditions: []v1alpha1.ConfigurationCondition{},
		},
	}}
}

func (bldr ConfigBuilder) Build() *v1alpha1.Configuration {
	return bldr.obj.DeepCopy()
}

func (bldr ConfigBuilder) Object() runtime.Object {
	return bldr.Build()
}

func (bldr ConfigBuilder) ToChildBuild(convert ConfigToBuildFunc) *buildv1alpha1.Build {
	return convert(bldr.Build())
}

func (bldr ConfigBuilder) ToChildRevision(convert ConfigToRevisionFunc) RevisionBuilder {
	return RevisionBuilder{convert(bldr.Build())}
}

func (bldr ConfigBuilder) ToUpdateAction() clientgotesting.UpdateActionImpl {
	action := clientgotesting.UpdateActionImpl{}
	action.Verb = "update"
	action.Object = bldr.Build()
	return action
}

func (bldr ConfigBuilder) WithGeneration(generation int64) ConfigBuilder {
	bldr.obj.Spec.Generation = generation
	return bldr
}

func (bldr ConfigBuilder) WithStatus(s v1alpha1.ConfigurationStatus) ConfigBuilder {
	bldr.obj.Status = s
	return bldr
}

func (bldr ConfigBuilder) WithBuild(b buildv1alpha1.BuildSpec) ConfigBuilder {
	bldr.obj.Spec.Build = &b
	return bldr
}

func (bldr ConfigBuilder) WithObservedGeneration(gen int64) ConfigBuilder {
	bldr.obj.Status.ObservedGeneration = gen
	return bldr
}

func (bldr ConfigBuilder) WithLatestCreatedRevisionName(name string) ConfigBuilder {
	bldr.obj.Status.LatestCreatedRevisionName = name
	return bldr
}

func (bldr ConfigBuilder) WithLatestReadyRevisionName(name string) ConfigBuilder {
	bldr.obj.Status.LatestReadyRevisionName = name
	return bldr
}

func (bldr ConfigBuilder) WithConcurrencyModel(cm v1alpha1.RevisionRequestConcurrencyModelType) ConfigBuilder {
	bldr.obj.Spec.RevisionTemplate.Spec.ConcurrencyModel = cm
	return bldr
}

func (bldr ConfigBuilder) WithReadyConditionUnknown() ConfigBuilder {
	bldr.obj.Status.Conditions = []v1alpha1.ConfigurationCondition{
		{
			Type:   v1alpha1.ConfigurationConditionReady,
			Status: corev1.ConditionUnknown,
		},
	}
	return bldr
}

func (bldr ConfigBuilder) WithReadyConditionTrue() ConfigBuilder {
	bldr.obj.Status.Conditions = []v1alpha1.ConfigurationCondition{
		{
			Type:   v1alpha1.ConfigurationConditionReady,
			Status: corev1.ConditionTrue,
		},
	}
	return bldr
}

func (bldr ConfigBuilder) WithReadyConditionFalse(reason, message string) ConfigBuilder {
	bldr.obj.Status.Conditions = []v1alpha1.ConfigurationCondition{
		{
			Type:    v1alpha1.ConfigurationConditionReady,
			Status:  corev1.ConditionFalse,
			Reason:  reason,
			Message: message,
		},
	}
	return bldr
}
