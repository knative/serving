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

	caching "knative.dev/caching/pkg/apis/caching/v1alpha1"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/revision/resources/names"
)

// MakeImageCache makes an caching.Image resources from a revision.
func MakeImageCache(rev *v1.Revision) *caching.Image {
	image := rev.Status.DeprecatedImageDigest
	if image == "" {
		image = rev.Spec.GetContainer().Image
	}

	img := &caching.Image{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.ImageCache(rev),
			Namespace: rev.Namespace,
			Labels:    makeLabels(rev),
			Annotations: kmeta.FilterMap(rev.GetAnnotations(), func(k string) bool {
				// Ignore last pinned annotation.
				return k == serving.RevisionLastPinnedAnnotationKey
			}),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(rev)},
		},
		Spec: caching.ImageSpec{
			Image:              image,
			ServiceAccountName: rev.Spec.ServiceAccountName,
			ImagePullSecrets:   rev.Spec.ImagePullSecrets,
		},
	}

	return img
}
