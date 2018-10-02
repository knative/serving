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

package resources

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/pkg/kmeta"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/configuration/resources/names"
)

func MakeBuild(config *v1alpha1.Configuration) *unstructured.Unstructured {
	if config.Spec.Build == nil {
		return nil
	}

	b := &buildv1alpha1.Build{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "build.knative.dev/v1alpha1",
			Kind:       "Build",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       config.Namespace,
			Name:            names.Build(config),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(config)},
		},
		Spec: *config.Spec.Build,
	}

	return MustToUnstructured(b)
}

func MustToUnstructured(build *buildv1alpha1.Build) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}

	b, err := json.Marshal(build)
	if err != nil {
		panic(err.Error())
	}

	if err := json.Unmarshal(b, u); err != nil {
		panic(err.Error())
	}

	return u
}

func UnstructuredWithContent(content map[string]interface{}) *unstructured.Unstructured {
	if content == nil {
		return nil
	}
	u := &unstructured.Unstructured{}
	u.SetUnstructuredContent(content)
	return u.DeepCopy()
}
