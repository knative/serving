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

package webhook

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"knative.dev/pkg/apis"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	"knative.dev/pkg/logging"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/revision/resources"
)

func decodeTemplate(val interface{}) (*v1.RevisionTemplateSpec, error) {
	templ := &v1.RevisionTemplateSpec{}
	asData, ok := val.(map[string]interface{})
	if !ok {
		return nil, errors.New("value could not be interpreted as a map")
	}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(asData, templ); err != nil {
		return nil, fmt.Errorf("could not decode RevisionTemplateSpec from resource: %w", err)
	}
	return templ, nil
}

func validatePodSpec(ctx context.Context, ps v1.RevisionSpec, namespace string, mode DryRunMode) *apis.FieldError {
	om := metav1.ObjectMeta{
		GenerateName: "dry-run-validation",
		Namespace:    namespace,
	}

	// Create a sample Revision from the template.
	rev := &v1.Revision{
		ObjectMeta: om,
		Spec:       ps,
	}
	rev.SetDefaults(ctx)
	podSpec := resources.BuildPodSpec(rev, resources.BuildUserContainers(rev), nil /*configs*/)

	// Make a sample pod with the template Revisions & PodSpec and dryrun call to API-server
	pod := &corev1.Pod{
		ObjectMeta: om,
		Spec:       *podSpec,
	}

	return dryRunPodSpec(ctx, pod, mode)
}

// dryRunPodSpec makes a dry-run call to k8s to validate the podspec
func dryRunPodSpec(ctx context.Context, pod *corev1.Pod, mode DryRunMode) *apis.FieldError {
	logger := logging.FromContext(ctx)
	client := kubeclient.Get(ctx)

	options := metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}
	if _, err := client.CoreV1().Pods(pod.GetNamespace()).Create(ctx, pod, options); err != nil {
		// Ignore failures for implementations that don't support dry-run.
		// This likely means there are other webhooks on the PodSpec Create action which do not declare sideEffects:none
		if mode != DryRunStrict && strings.Contains(err.Error(), "does not support dry run") {
			logger.Warnw("dry run validation failed, a webhook did not support dry-run", zap.Error(err))
			return nil
		}

		return apis.ErrGeneric("dry run failed with "+err.Error(), "spec.template")
	}
	return nil
}
