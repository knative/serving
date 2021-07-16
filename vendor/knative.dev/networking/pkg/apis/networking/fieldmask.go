/*
Copyright 2020 The Knative Authors

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

package networking

import (
	corev1 "k8s.io/api/core/v1"
)

// NamespacedObjectReferenceMask performs a _shallow_ copy of the Kubernetes ObjectReference
// object to a new Kubernetes ObjectReference object bringing over only the fields allowed in
// the Knative API. This does not validate the contents or the bounds of the provided fields.
func NamespacedObjectReferenceMask(in *corev1.ObjectReference) *corev1.ObjectReference {
	if in == nil {
		return nil
	}

	out := new(corev1.ObjectReference)

	// Allowed fields
	out.APIVersion = in.APIVersion
	out.Kind = in.Kind
	out.Name = in.Name

	// Disallowed
	// This list is unnecessary, but added here for clarity
	out.Namespace = ""
	out.FieldPath = ""
	out.ResourceVersion = ""
	out.UID = ""

	return out
}
