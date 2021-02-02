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

import (
	"fmt"

	"knative.dev/pkg/apis"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
)

// ScaleResourceArguments returns the GroupVersionResource and resource name from an ObjectReference.
func ScaleResourceArguments(ref corev1.ObjectReference) (gvr *schema.GroupVersionResource, name string, err error) {
	gv, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil {
		return nil, "", err
	}
	resource := apis.KindToResource(gv.WithKind(ref.Kind))
	return &resource, ref.Name, nil
}

// GetScaleResource returns the current scale resource for the PA.
// TODO(markusthoemmes): We shouldn't need to pass namespace here.
func GetScaleResource(namespace string, ref corev1.ObjectReference, listerFactory func(schema.GroupVersionResource) (cache.GenericLister, error)) (*autoscalingv1alpha1.PodScalable, error) {
	gvr, name, err := ScaleResourceArguments(ref)
	if err != nil {
		return nil, fmt.Errorf("error getting the scale arguments: %w", err)
	}
	lister, err := listerFactory(*gvr)
	if err != nil {
		return nil, fmt.Errorf("error getting a lister for a pod scalable resource '%+v': %w", gvr, err)
	}

	psObj, err := lister.ByNamespace(namespace).Get(name)
	if err != nil {
		return nil, fmt.Errorf("error fetching Pod Scalable %s/%s: %w", namespace, name, err)
	}
	return psObj.(*autoscalingv1alpha1.PodScalable), nil
}
