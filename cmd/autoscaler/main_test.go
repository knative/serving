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
	autoscalerfake "knative.dev/serving/pkg/autoscaler/fake"
	"knative.dev/serving/pkg/autoscaler/scaling"
)

var (
	kubeClient   = fakek8s.NewSimpleClientset()
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
)

func TestUniscalerFactoryFailures(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{{
		"nil labels", nil, fmt.Sprintf("label %q not found or empty in Decider", serving.ConfigurationLabelKey),
	}, {
		"empty labels", map[string]string{}, fmt.Sprintf("label %q not found or empty in Decider", serving.ConfigurationLabelKey),
	}, {
		"rev missing", map[string]string{
			"some-unimportant-label":      "lo-digo",
			serving.ServiceLabelKey:       "la",
			serving.ConfigurationLabelKey: "bamba",
		},
		fmt.Sprintf("label %q not found or empty in Decider", serving.RevisionLabelKey),
	}, {
		"config missing", map[string]string{
			"some-unimportant-label": "lo-digo",
			serving.ServiceLabelKey:  "la",
			serving.RevisionLabelKey: "bamba",
		},
		fmt.Sprintf("label %q not found or empty in Decider", serving.ConfigurationLabelKey),
	}, {
		"values not ascii", map[string]string{
			serving.ServiceLabelKey:       "la",
			serving.ConfigurationLabelKey: "verité",
			serving.RevisionLabelKey:      "bamba",
		}, "invalid value: only ASCII characters accepted",
	}, {
		"too long of a value", map[string]string{
			serving.ServiceLabelKey:       "cat is ",
			serving.RevisionLabelKey:      "bamba",
			serving.ConfigurationLabelKey: "l" + strings.Repeat("o", 253) + "ng",
		}, "max length must be 255 characters",
	}}

	uniScalerFactory := testUniScalerFactory()
	decider := &scaling.Decider{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: autoscalerfake.TestNamespace,
			Name:      autoscalerfake.TestRevision,
		},
		Spec: scaling.DeciderSpec{
			ServiceName: "wholesome-service",
		},
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
	autoscalerfake.Endpoints(1, "magic-services-offered")
	uniScalerFactory := testUniScalerFactory()
	for _, srv := range []string{"some", ""} {
		decider := &scaling.Decider{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: autoscalerfake.TestNamespace,
				Name:      autoscalerfake.TestRevision,
				Labels: map[string]string{
					serving.RevisionLabelKey:      autoscalerfake.TestRevision,
					serving.ServiceLabelKey:       srv,
					serving.ConfigurationLabelKey: "test-config",
				},
			},
			Spec: scaling.DeciderSpec{
				ServiceName: "magic-services-offered",
			},
		}

		if _, err := uniScalerFactory(decider); err != nil {
			t.Errorf("got error from uniScalerFactory: %v", err)
		}
	}
}

func testUniScalerFactory() func(decider *scaling.Decider) (scaling.UniScaler, error) {
	return uniScalerFactoryFunc(kubeInformer.Core().V1().Pods().Lister(), &autoscalerfake.MetricClient{})
}
