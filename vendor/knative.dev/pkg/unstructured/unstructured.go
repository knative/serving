/*
Copyright 2021 The Knative Authors

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

package unstructured

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// ConvertTo converts a runtime.Object to an unstructured.Unstructured type
func ConvertTo(s *runtime.Scheme, obj runtime.Object) (*unstructured.Unstructured, error) {
	var (
		err error
		u   unstructured.Unstructured
	)

	u.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to unstructured: %w", err)
	}

	gvk := u.GroupVersionKind()
	if gvk.Group == "" || gvk.Kind == "" {
		gvks, _, err := s.ObjectKinds(obj)
		if err != nil {
			return nil, fmt.Errorf("failed to convert to unstructured: %w", err)
		}
		apiv, k := gvks[0].ToAPIVersionAndKind()
		u.SetAPIVersion(apiv)
		u.SetKind(k)
	}
	return &u, nil
}

// ConvertManyTo converts a slice of runtime.Object to a slice of *unstructured.Unstructured
func ConvertManyTo(s *runtime.Scheme, objs []runtime.Object) ([]*unstructured.Unstructured, error) {
	ul := make([]*unstructured.Unstructured, 0, len(objs))

	for _, obj := range objs {
		u, err := ConvertTo(s, obj)
		if err != nil {
			return nil, err
		}

		ul = append(ul, u)
	}
	return ul, nil
}

// ConvertManyToObjects converts a slice of runtime.Object to a slice of runtime.Objects
// where each element is of the type *unstructured.Unstructured
func ConvertManyToObjects(s *runtime.Scheme, objs []runtime.Object) ([]runtime.Object, error) {
	ul := make([]runtime.Object, 0, len(objs))

	for _, obj := range objs {
		u, err := ConvertTo(s, obj)
		if err != nil {
			return nil, err
		}

		ul = append(ul, u)
	}
	return ul, nil
}
