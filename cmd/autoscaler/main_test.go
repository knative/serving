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
	"testing"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/autoscaler"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	fakeK8s "k8s.io/client-go/kubernetes/fake"
)

const (
	testNamespace = "test-namespace"
	testRevision  = "test-Revision"
)

func TestLabelValueOrEmpty(t *testing.T) {
	metric := &autoscaler.Metric{}
	metric.Labels = make(map[string]string)
	metric.Labels["test1"] = "test1val"
	metric.Labels["test2"] = ""

	cases := []struct {
		name string
		key  string
		want string
	}{{
		name: "existing key",
		key:  "test1",
		want: "test1val",
	}, {
		name: "existing empty key",
		key:  "test2",
		want: "",
	}, {
		name: "non-existent key",
		key:  "test4",
		want: "",
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := labelValueOrEmpty(metric, c.key); got != c.want {
				t.Errorf("%q expected: %v got: %v", c.name, got, c.want)
			}
		})
	}
}

func TestUniScalerFactoryFunc(t *testing.T) {
	uniScalerFactory := getTestUniScalerFactory()
	metric := &autoscaler.Metric{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRevision,
			Labels:    map[string]string{serving.RevisionLabelKey: testRevision},
		},
	}
	dynamicConfig := &autoscaler.DynamicConfig{}

	if _, err := uniScalerFactory(metric, dynamicConfig); err != nil {
		t.Errorf("got error from uniScalerFactory: %v", err)
	}
}

func TestUniScalerFactoryFunc_FailWhenRevisionLabelMissing(t *testing.T) {
	uniScalerFactory := getTestUniScalerFactory()
	metric := &autoscaler.Metric{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRevision,
		},
	}
	dynamicConfig := &autoscaler.DynamicConfig{}

	if _, err := uniScalerFactory(metric, dynamicConfig); err == nil {
		t.Errorf("expected error when revision label missing but got none")
	}
}

func getTestUniScalerFactory() func(metric *autoscaler.Metric, dynamicConfig *autoscaler.DynamicConfig) (autoscaler.UniScaler, error) {
	kubeClient := fakeK8s.NewSimpleClientset()
	kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	return uniScalerFactoryFunc(kubeInformer.Core().V1().Endpoints())
}
