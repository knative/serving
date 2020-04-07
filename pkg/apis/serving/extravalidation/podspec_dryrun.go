/*
Copyright 2020 The Knative Authors.

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

package extravalidation

import (
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/apis"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	"knative.dev/pkg/system"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/revision/resources"
)

// ExtraServiceValidation runs extra validation on Service resources
func ExtraServiceValidation(ctx context.Context, uns *unstructured.Unstructured) error {
	s := v1.Service{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uns.UnstructuredContent(), s); err != nil {
		return errors.New("could not decode Service from resource")
	}

	om := metav1.ObjectMeta{
		Name:      "dry-run-validation",
		Namespace: system.Namespace(),
	}

	// Create a dummy Revision from the template
	rev := &v1.Revision{
		ObjectMeta: om,
		Spec:       *&s.Spec.Template.Spec,
	}
	userContainer := resources.BuildUserContainer(rev)
	podSpec := resources.BuildPodSpec(rev, []corev1.Container{*userContainer})

	// Make a dummy pod with the template Revions & PodSpec and dryrun call to API-server
	pod := &corev1.Pod{
		ObjectMeta: om,
		Spec:       *podSpec,
	}

	dryRunPodSpec(ctx, pod)
	return nil
}

// dryRunPodSpec makes a dry-run call to k8s to validate the podspec
func dryRunPodSpec(ctx context.Context, pod *corev1.Pod) *apis.FieldError {
	client := kubeclient.Get(ctx)
	pods := client.CoreV1().Pods(pod.GetNamespace())
	options := metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}
	if _, err := pods.CreateWithOptions(ctx, pod, options); err != nil {
		return apis.ErrGeneric("PodSpec dry run failed: "+err.Error(), "PodSpec")
	}
}
