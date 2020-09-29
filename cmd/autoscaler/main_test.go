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

package main

import (
	"fmt"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	fakek8s "k8s.io/client-go/kubernetes/fake"

	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/autoscaler/scaling"
)

var kubeInformer = kubeinformers.NewSharedInformerFactory(fakek8s.NewSimpleClientset(), 0)

func TestUniscalerFactoryFailures(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{{
		name:   "nil labels",
		labels: nil,
		want:   fmt.Sprintf("label %q not found or empty in Decider", serving.ConfigurationLabelKey),
	}, {
		name:   "empty labels",
		labels: map[string]string{},
		want:   fmt.Sprintf("label %q not found or empty in Decider", serving.ConfigurationLabelKey),
	}, {
		name: "rev missing",
		labels: map[string]string{
			"some-unimportant-label":      "lo-digo",
			serving.ServiceLabelKey:       "la",
			serving.ConfigurationLabelKey: "bamba",
		},
		want: fmt.Sprintf("label %q not found or empty in Decider", serving.RevisionLabelKey),
	}, {
		name: "config missing",
		labels: map[string]string{
			"some-unimportant-label": "lo-digo",
			serving.ServiceLabelKey:  "la",
			serving.RevisionLabelKey: "bamba",
		},
		want: fmt.Sprintf("label %q not found or empty in Decider", serving.ConfigurationLabelKey),
	}}

	uniScalerFactory := testUniScalerFactory()
	decider := &scaling.Decider{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "a-cool-namespace",
			Name:      "very-nice-revision-name",
		},
		Spec: scaling.DeciderSpec{},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decider.Labels = test.labels

			_, err := uniScalerFactory(decider)
			if err == nil {
				t.Fatal("No error was returned")
			}
			if got, want := err.Error(), test.want; !strings.Contains(got, want) {
				t.Errorf("Error = %q, want to contain = %q", got, want)
			}
		})
	}
}

func TestUniScalerFactoryFunc(t *testing.T) {
	uniScalerFactory := testUniScalerFactory()
	for _, srv := range []string{"some", ""} {
		decider := &scaling.Decider{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ome-more-namespace",
				Name:      "astounding-revision",
				Labels: map[string]string{
					serving.RevisionLabelKey:      "astrounding-revision",
					serving.ServiceLabelKey:       srv,
					serving.ConfigurationLabelKey: "test-config",
				},
			},
			Spec: scaling.DeciderSpec{},
		}

		if _, err := uniScalerFactory(decider); err != nil {
			t.Error("got error from uniScalerFactory:", err)
		}
	}
}

func testUniScalerFactory() func(decider *scaling.Decider) (scaling.UniScaler, error) {
	return uniScalerFactoryFunc(kubeInformer.Core().V1().Pods().Lister(), nil)
}
