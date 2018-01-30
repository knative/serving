/*
Copyright 2017 The Kubernetes Authors.

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

package util

import (
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

const (
	// GroupName is a name of a Kubernetes API extension implemented by WarmImage.
	WarmImageGroupName = "mattmoor.io"

	// APIVersion is a version of the Kubernetes API extension implemented by WarmImage.
	WarmImageAPIVersion = "v1"

	// FullApiVersion is necessary for TypeMeta.Kind
	WarmImageFullAPIVersion = WarmImageGroupName + "/" + WarmImageAPIVersion

	// Kind of the TPR resource that WarmImage exposes
	WarmImageKind = "WarmImage"
)

var WarmImageResource = meta_v1.APIResource{
	Name:       "warmimages",
	Kind:       WarmImageKind,
	Namespaced: false,
}

// WarmImage looks like so:
//   spec:
//     image: gcr.io/google-appengine/debian8:latest
type WarmImageSpec struct {
	Image string `json:"image"`
	// TODO(mattmoor): ImagePullSecrets
}

type WarmImage struct {
	meta_v1.TypeMeta
	Metadata meta_v1.ObjectMeta `json:"metadata"`
	Spec     WarmImageSpec      `json:"spec"`
}

func CreateWarmImageClient(config *rest.Config) *dynamic.Client {
	return CreateCustomResourceClient(config, &schema.GroupVersion{
		Group:   WarmImageGroupName,
		Version: WarmImageAPIVersion,
	})
}
