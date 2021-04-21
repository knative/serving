/*
Copyright 2018 The Knative Authors.

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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/kmeta"
)

// +genclient
// +genreconciler
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Image is a Knative abstraction that encapsulates the interface by which Knative
// components express a desire to have a particular image cached.
type Image struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec holds the desired state of the Image (from the client).
	// +optional
	Spec ImageSpec `json:"spec,omitempty"`

	// Status communicates the observed state of the Image (from the controller).
	// +optional
	Status ImageStatus `json:"status,omitempty"`
}

// Check that Image can be validated and defaulted.
var _ apis.Validatable = (*Image)(nil)
var _ apis.Defaultable = (*Image)(nil)
var _ kmeta.OwnerRefable = (*Image)(nil)
var _ duckv1.KRShaped = (*Image)(nil)

// ImageSpec holds the desired state of the Image (from the client).
type ImageSpec struct {

	// Image is the name of the container image url to cache across the cluster.
	Image string `json:"image"`

	// ServiceAccountName is the name of the Kubernetes ServiceAccount as which the Pods
	// will run this container.  This is potentially used to authenticate the image pull
	// if the service account has attached pull secrets.  For more information:
	// https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#add-imagepullsecrets-to-a-service-account
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// ImagePullSecrets contains the names of the Kubernetes Secrets containing login
	// information used by the Pods which will run this container.
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

// ImageStatus communicates the observed state of the Image (from the controller).
type ImageStatus struct {
	duckv1.Status `json:",inline"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ImageList is a list of Image resources
type ImageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Image `json:"items"`
}
