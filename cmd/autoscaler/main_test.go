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

func TestUniScalerFactoryFunc(t *testing.T) {
	uniScalerFactory := getTestUniScalerFactory()
	metric := &autoscaler.Decider{
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
	metric := &autoscaler.Decider{
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

func getTestUniScalerFactory() func(decider *autoscaler.Decider, dynamicConfig *autoscaler.DynamicConfig) (autoscaler.UniScaler, error) {
	kubeClient := fakeK8s.NewSimpleClientset()
	kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	return uniScalerFactoryFunc(kubeInformer.Core().V1().Endpoints())
}
