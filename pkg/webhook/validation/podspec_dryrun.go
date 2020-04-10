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

package validation

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/apis"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	"knative.dev/pkg/logging"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/revision/resources"
)

// ExtraServiceValidation runs extra validation on Service resources
func ExtraServiceValidation(ctx context.Context, uns *unstructured.Unstructured) error {
	logger := logging.FromContext(ctx)

	val, found, err := unstructured.NestedFieldNoCopy(uns.UnstructuredContent(), "spec", "template")
	if err != nil {
		return fmt.Errorf("could not traverse nested spec.template field: %w", err)
	}
	if found {
		templ := &v1.RevisionTemplateSpec{}
		asData := val.(map[string]interface{})
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(asData, templ); err != nil {
			return fmt.Errorf("could not decode RevisionTemplateSpec from resource: %w", err)
		}

		if err := validatePodSpec(ctx, templ.Spec, templ.ObjectMeta.Namespace); err != nil {
			return err
		}
	}

	logger.Warnw("no spec.template found for unstrucutred", uns)
	return nil
}

func validatePodSpec(ctx context.Context, ps v1.RevisionSpec, namespace string) *apis.FieldError {
	om := metav1.ObjectMeta{
		Name:      "dry-run-validation",
		Namespace: namespace,
	}

	// Create a dummy Revision from the template
	rev := &v1.Revision{
		ObjectMeta: om,
		Spec:       ps,
	}
	userContainer := resources.BuildUserContainer(rev)
	podSpec := resources.BuildPodSpec(rev, []corev1.Container{*userContainer})

	// Make a dummy pod with the template Revions & PodSpec and dryrun call to API-server
	pod := &corev1.Pod{
		ObjectMeta: om,
		Spec:       *podSpec,
	}

	return dryRunPodSpec(ctx, pod)
}

// dryRunPodSpec makes a dry-run call to k8s to validate the podspec
func dryRunPodSpec(ctx context.Context, pod *corev1.Pod) *apis.FieldError {
	logger := logging.FromContext(ctx)
	client := kubeclient.Get(ctx)
	pods := client.CoreV1().Pods(pod.GetNamespace())

	options := metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}
	if _, err := pods.CreateWithOptions(ctx, pod, options); err != nil {
		// Ignore failures for implementations that don't support dry-run.
		// This likely means there are other webhooks on the PodSpec Create action which do not declare sideEffects:none
		if strings.Contains(err.Error(), "does not support dry run") {
			logger.Errorw("dry run validation failed, a webhook did not support dry-run", zap.Error(err))
			return nil
		}

		return apis.ErrGeneric("podSpec dry run failed", err.Error())
	}
	return nil
}
