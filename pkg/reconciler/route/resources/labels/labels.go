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

package labels

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	network "knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
)

// IsObjectLocalVisibility returns whether an ObjectMeta is of cluster-local visibility
func IsObjectLocalVisibility(meta *metav1.ObjectMeta) bool {
	return meta.Labels[network.VisibilityLabelKey] != ""
}

// SetVisibility sets the visibility on an ObjectMeta
func SetVisibility(meta *metav1.ObjectMeta, isClusterLocal bool) {
	if isClusterLocal {
		SetLabel(meta, network.VisibilityLabelKey, serving.VisibilityClusterLocal)
	} else {
		DeleteLabel(meta, network.VisibilityLabelKey)
	}
}

// GetVisibility maps the label value for VisibilityLabelKey to the networking constants.
// Uses the comma-ok idom, it will return true as second return value of the annotation was found
// and false if it was missing.
func GetVisibility(meta *metav1.ObjectMeta) (v1alpha1.IngressVisibility, bool) {
	switch meta.Labels[network.VisibilityLabelKey] {
	case serving.VisibilityExternalIP:
		return v1alpha1.IngressVisibilityExternalIP, true
	case serving.VisibilityClusterLocal:
		return v1alpha1.IngressVisibilityClusterLocal, true
	}
	return "", false
}

// SetLabel sets/update the label of the an ObjectMeta
func SetLabel(meta *metav1.ObjectMeta, key, value string) {
	if meta.Labels == nil {
		meta.Labels = make(map[string]string, 1)
	}

	meta.Labels[key] = value
}

// DeleteLabel removes a label from the ObjectMeta
func DeleteLabel(meta *metav1.ObjectMeta, key string) {
	delete(meta.Labels, key)
}
