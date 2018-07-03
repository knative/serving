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

package controller

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

func kind(obj metav1.Object) schema.GroupVersionKind {
	switch obj.(type) {
	case *v1alpha1.Service:
		return v1alpha1.SchemeGroupVersion.WithKind("Service")
	case *v1alpha1.Route:
		return v1alpha1.SchemeGroupVersion.WithKind("Route")
	case *v1alpha1.Configuration:
		return v1alpha1.SchemeGroupVersion.WithKind("Configuration")
	case *v1alpha1.Revision:
		return v1alpha1.SchemeGroupVersion.WithKind("Revision")
	default:
		panic(fmt.Sprintf("Unsupported object type %T", obj))
	}
}

// NewControllerRef creates an OwnerReference pointing to the given Resource.
func NewControllerRef(obj metav1.Object) *metav1.OwnerReference {
	return metav1.NewControllerRef(obj, kind(obj))
}
