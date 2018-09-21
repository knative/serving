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
	"github.com/knative/pkg/kmeta"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/configuration/resources/names"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func MakeBuild(config *v1alpha1.Configuration) *unstructured.Unstructured {
	if config.Spec.Build == nil {
		return nil
	}
	u := &unstructured.Unstructured{}
	u.SetUnstructuredContent(map[string]interface{}{"spec": config.Spec.Build.DeepCopy().Object})
	u.SetNamespace(config.Namespace)
	u.SetName(names.Build(config))
	u.SetOwnerReferences([]metav1.OwnerReference{*kmeta.NewControllerRef(config)})
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
