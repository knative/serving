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
	corev1 "k8s.io/api/core/v1"

	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/service/resources/names"
)

func TestRouteSpec(t *testing.T) {
	s := createService()
	testConfigName := names.Configuration(s)
	r := MakeRoute(s)
	if got, want := r.Name, testServiceName; got != want {
		t.Errorf("Expected %q for service name got %q", want, got)
	}
	if got, want := r.Namespace, testServiceNamespace; got != want {
		t.Errorf("Expected %q for service namespace got %q", want, got)
	}
	if got, want := len(r.Spec.Traffic), 1; got != want {
		t.Fatalf("Expected %d traffic targets got %d", want, got)
	}
	wantT := []v1.TrafficTarget{{
		Percent:           ptr.Int64(100),
		ConfigurationName: testConfigName,
		LatestRevision:    ptr.Bool(true),
	}}
	if got, want := r.Spec.Traffic, wantT; !cmp.Equal(got, want) {
		t.Error("Traffic mismatch: diff (-got, +want):", cmp.Diff(got, want))
	}
	expectOwnerReferencesSetCorrectly(t, r.OwnerReferences)

	if got, want := len(r.Labels), 1; got != want {
		t.Errorf("expected %d labels got %d", want, got)
	}
	if got, want := r.Labels[serving.ServiceLabelKey], testServiceName; got != want {
		t.Errorf("expected %q labels got %q", want, got)
	}
}

func TestRouteHasNoKubectlAnnotation(t *testing.T) {
	s := createServiceWithKubectlAnnotation()
	r := MakeRoute(s)
	if v, ok := r.Annotations[corev1.LastAppliedConfigAnnotation]; ok {
		t.Errorf("Annotation %s = %q, want empty", corev1.LastAppliedConfigAnnotation, v)
	}
}
