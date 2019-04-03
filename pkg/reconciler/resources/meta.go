/*
Copyright 2019 The Knative Authors

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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// MakeLabels returns a label map constructed from the union of labels of
// the parent object and additional labels.
func MakeLabels(obj metav1.Object, add map[string]string) map[string]string {
	labels := make(map[string]string, len(obj.GetLabels()))

	for k, v := range obj.GetLabels() {
		labels[k] = v
	}
	for k, v := range add {
		labels[k] = v
	}
	return labels
}

// MakeAnnotations creates the annotations we will apply to the
// child resource of the given resource, if filter is provided and returns true,
// the annotation will be dropped.
func MakeAnnotations(obj metav1.Object, filter func(string) bool) map[string]string {
	annotations := make(map[string]string, len(obj.GetAnnotations()))
	for k, v := range obj.GetAnnotations() {
		if filter != nil && filter(k) {
			continue
		}
		annotations[k] = v
	}
	return annotations
}
