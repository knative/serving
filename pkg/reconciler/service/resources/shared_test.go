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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/ptr"
	v1 "knative.dev/serving/pkg/apis/serving/v1"

	. "knative.dev/serving/pkg/testing/v1"
)

const (
	testServiceName      = "test-service"
	testServiceNamespace = "test-service-namespace"
	testContainerName    = "test-container"
	testAnnotationValue  = "test-annotation-value"
)

func expectOwnerReferencesSetCorrectly(t *testing.T, ownerRefs []metav1.OwnerReference) {
	t.Helper()
	if got, want := len(ownerRefs), 1; got != want {
		t.Errorf("expected %d owner refs got %d", want, got)
		return
	}

	expectedRefs := []metav1.OwnerReference{{
		APIVersion: "serving.knative.dev/v1",
		Kind:       "Service",
		Name:       testServiceName,
	}}
	if diff := cmp.Diff(expectedRefs, ownerRefs, cmpopts.IgnoreFields(expectedRefs[0], "Controller", "BlockOwnerDeletion")); diff != "" {
		t.Error("Unexpected service owner refs diff (-want +got):", diff)
	}
}

func createConfiguration(containerName string) *v1.ConfigurationSpec {
	return &v1.ConfigurationSpec{
		Template: v1.RevisionTemplateSpec{
			Spec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: containerName,
					}},
				},
			},
		},
	}
}

func createService() *v1.Service {
	return DefaultService(testServiceName, testServiceNamespace,
		WithConfigSpec(createConfiguration(testContainerName)),
		WithRouteSpec(v1.RouteSpec{
			Traffic: []v1.TrafficTarget{{
				Percent: ptr.Int64(100),
			}},
		}),
	)
}

func createServiceWithKubectlAnnotation() *v1.Service {
	return DefaultService(testServiceName, testServiceNamespace,
		WithConfigSpec(createConfiguration(testContainerName)),
		WithServiceAnnotations(map[string]string{
			corev1.LastAppliedConfigAnnotation: testAnnotationValue,
		}))
}
