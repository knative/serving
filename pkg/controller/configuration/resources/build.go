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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/controller/configuration/resources/names"
)

func MakeBuild(config *v1alpha1.Configuration) *buildv1alpha1.Build {
	if config.Spec.Build == nil {
		return nil
	}
	return &buildv1alpha1.Build{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       config.Namespace,
			Name:            names.Build(config),
			OwnerReferences: []metav1.OwnerReference{*controller.NewControllerRef(config)},
		},
		Spec: *config.Spec.Build,
	}
}
