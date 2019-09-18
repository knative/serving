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
	"knative.dev/serving/pkg/apis/serving/v1alpha1"

	. "knative.dev/serving/pkg/testing/v1alpha1"
)

const (
	testServiceName            = "test-service"
	testServiceNamespace       = "test-service-namespace"
	testRevisionName           = "test-revision-name"
	testCandidateRevisionName  = "test-candidate-revision-name"
	testContainerNameRunLatest = "test-container-run-latest"
	testContainerNamePinned    = "test-container-pinned"
	testContainerNameRelease   = "test-container-release"
	testContainerNameInline    = "test-container-inline"
	testLabelKey               = "test-label-key"
	testLabelValuePinned       = "test-label-value-pinned"
	testLabelValueRunLatest    = "test-label-value-run-latest"
	testLabelValueRelease      = "test-label-value-release"
	testAnnotationKey          = "test-annotation-key"
	testAnnotationValue        = "test-annotation-value"
)

func expectOwnerReferencesSetCorrectly(t *testing.T, ownerRefs []metav1.OwnerReference) {
	t.Helper()
	if got, want := len(ownerRefs), 1; got != want {
		t.Errorf("expected %d owner refs got %d", want, got)
		return
	}

	expectedRefs := []metav1.OwnerReference{{
		APIVersion: "serving.knative.dev/v1alpha1",
		Kind:       "Service",
		Name:       testServiceName,
	}}
	if diff := cmp.Diff(expectedRefs, ownerRefs, cmpopts.IgnoreFields(expectedRefs[0], "Controller", "BlockOwnerDeletion")); diff != "" {
		t.Errorf("Unexpected service owner refs diff (-want +got): %v", diff)
	}
}

func createConfiguration(containerName string) v1alpha1.ConfigurationSpec {
	return v1alpha1.ConfigurationSpec{
		DeprecatedRevisionTemplate: &v1alpha1.RevisionTemplateSpec{
			Spec: v1alpha1.RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Name: containerName,
				},
			},
		},
	}
}

func createServiceInline() *v1alpha1.Service {
	return Service(testServiceName, testServiceNamespace,
		WithInlineConfigSpec(createConfiguration(testContainerNameInline)),
		WithInlineRouteSpec(v1alpha1.RouteSpec{
			Traffic: []v1alpha1.TrafficTarget{{
				TrafficTarget: v1.TrafficTarget{
					Percent: ptr.Int64(100),
				},
			}},
		}))
}

func createServiceWithRunLatest() *v1alpha1.Service {
	return Service(testServiceName, testServiceNamespace,
		WithRunLatestConfigSpec(createConfiguration(testContainerNameRunLatest)),
		WithServiceLabel(testLabelKey, testLabelValueRunLatest),
		WithServiceAnnotations(map[string]string{
			testAnnotationKey: testAnnotationValue,
		}))
}

func createServiceWithPinned() *v1alpha1.Service {
	return Service(testServiceName, testServiceNamespace,
		WithPinnedRolloutConfigSpec(testRevisionName, createConfiguration(testContainerNamePinned)),
		WithServiceLabel(testLabelKey, testLabelValuePinned))
}

func createServiceWithRelease(numRevision int, rolloutPercent int) *v1alpha1.Service {
	var revisions []string
	if numRevision == 2 {
		revisions = []string{testRevisionName, testCandidateRevisionName}
	} else {
		revisions = []string{testRevisionName}
	}

	return Service(testServiceName, testServiceNamespace,
		WithReleaseRolloutAndPercentageConfigSpec(rolloutPercent, createConfiguration(testContainerNameRelease), revisions...),
		WithServiceLabel(testLabelKey, testLabelValueRelease))
}

func createServiceWithKubectlAnnotation() *v1alpha1.Service {
	return Service(testServiceName, testServiceNamespace,
		WithRunLatestConfigSpec(createConfiguration(testContainerNameRunLatest)),
		WithServiceAnnotations(map[string]string{
			corev1.LastAppliedConfigAnnotation: testAnnotationValue,
		}))
}
