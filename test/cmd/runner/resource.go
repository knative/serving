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

package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/resource"
)

type resourceInfo struct {
	*resource.Info

	object       *unstructured.Unstructured
	serverObject *unstructured.Unstructured
	applied      bool
}

func (r *resourceInfo) isPatch() bool {
	return strings.HasSuffix(r.Source, ".patch")
}

func (r *resourceInfo) helper() *resource.Helper {
	return resource.NewHelper(r.Client, r.Mapping)
}

func (r *resourceInfo) json() ([]byte, error) {
	var buffer bytes.Buffer
	err := unstructured.UnstructuredJSONScheme.Encode(r.object, &buffer)
	return buffer.Bytes(), err
}

func (r *resourceInfo) String() string {
	return r.ObjectName()
}

func (r *resourceInfo) apply(ctx context.Context) error {
	if r.isPatch() {
		json, err := r.json()
		if err != nil {
			return fmt.Errorf("failed to serialize resource '%q': %w", r, err)
		}
		_, err = r.helper().Patch(
			r.Namespace,
			r.Name,
			types.StrategicMergePatchType,
			json,
			&metav1.PatchOptions{})

		return err

	}
	_, err := r.helper().Create(r.Namespace, false, r.object)
	return err
}

func (r *resourceInfo) revert(ctx context.Context) error {
	if r.isPatch() {
		_, err := r.helper().Replace(r.Namespace, r.Name, true, r.serverObject)
		return err
	}

	_, err := r.helper().Delete(r.Namespace, r.Name)
	return err
}
