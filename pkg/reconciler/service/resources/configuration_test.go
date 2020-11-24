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
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"

	"knative.dev/serving/pkg/apis/serving"
)

func TestConfigurationSpec(t *testing.T) {
	s := createService()
	c := MakeConfiguration(s)
	if got, want := c.Name, testServiceName; got != want {
		t.Errorf("Service name = %q; want: %q", got, want)
	}
	if got, want := c.Namespace, testServiceNamespace; got != want {
		t.Errorf("Service namespace = %q; want: %q", got, want)
	}
	if got, want := c.Spec.GetTemplate().Spec.GetContainer().Name, testContainerName; got != want {
		t.Errorf("Container name = %q; want: %q", got, want)
	}
	expectOwnerReferencesSetCorrectly(t, c.OwnerReferences)

	if got, want := c.Labels, map[string]string{serving.ServiceLabelKey: testServiceName}; !cmp.Equal(got, want) {
		t.Errorf("Labels mismatch: diff(-want,+got):\n%s", cmp.Diff(want, got))
	}
	if got, want := c.Annotations, map[string]string{
		serving.RoutesAnnotationKey: testServiceName,
	}; !cmp.Equal(got, want) {
		t.Errorf("Annotations mismatch: diff(-want,+got):\n%s", cmp.Diff(want, got))
	}

	// Create the configuration based on the same existing configuration.
	c = MakeConfigurationFromExisting(s, c)
	if got, want := c.Annotations, map[string]string{
		serving.RoutesAnnotationKey: testServiceName,
	}; !cmp.Equal(got, want) {
		t.Errorf("Annotations mismatch: diff(-want,+got):\n%s", cmp.Diff(want, got))
	}

	// Create the configuration based on the configuration with a different value for the
	// annotation key serving.RoutesAnnotationKey.
	const secTestServiceName = "second-test-service"
	secondConfig := MakeConfigurationFromExisting(
		createServiceWithName(secTestServiceName), c)

	// MakeConfigurationFromExisting employs maps in process, so order is
	// not guaranteed.
	annoValue := secondConfig.Annotations[serving.RoutesAnnotationKey]
	got := strings.Split(annoValue, ",")
	sort.Strings(got)

	want := []string{secTestServiceName, testServiceName}
	if !cmp.Equal(got, want) {
		t.Errorf("Annotations = %v, want: %v", got, want)
	}
}

func TestConfigurationHasNoKubectlAnnotation(t *testing.T) {
	s := createServiceWithKubectlAnnotation()
	c := MakeConfiguration(s)
	if v, ok := c.Annotations[corev1.LastAppliedConfigAnnotation]; ok {
		t.Errorf(`Annotation[%s] = %q, want: ""`, corev1.LastAppliedConfigAnnotation, v)
	}
}
