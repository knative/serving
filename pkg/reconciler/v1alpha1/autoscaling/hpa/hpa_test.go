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

package hpa

import (
	"testing"

	"github.com/knative/pkg/controller"
	autoscalingv1alpha1 "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/autoscaling/hpa/resources"
	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgotesting "k8s.io/client-go/testing"
)

const (
	testNamespace = "test-namespace"
	testRevision  = "test-revision"
)

func TestReconcile(t *testing.T) {
	table := TableTest{{
		Name: "create hpa",
		Objects: []runtime.Object{
			pa(testRevision, testNamespace, WithHPAClass),
		},
		Key: key(testRevision, testNamespace),
		WantCreates: []metav1.Object{
			hpa(testRevision, testNamespace, WithHPAClass, WithMetricAnnotation("cpu")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: pa(testRevision, testNamespace, WithHPAClass, WithTraffic),
		}},
	}, {
		Name: "do not create hpa when non-hpa-class pod autoscaler",
		Objects: []runtime.Object{
			pa(testRevision, testNamespace, WithKPAClass),
		},
		Key: key(testRevision, testNamespace),
	}, {
		Name:    "delete when pa does not exist",
		Objects: []runtime.Object{},
		Key:     key(testRevision, testNamespace),
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: testNamespace,
				Verb:      "delete",
				Resource: schema.GroupVersionResource{
					Group:    "autoscaling",
					Version:  "v1",
					Resource: "horizontalpodautoscalers",
				},
			},
			Name: testRevision,
		}},
	}, {
		Name: "update hpa with target usage",
		Objects: []runtime.Object{
			pa(testRevision, testNamespace, WithHPAClass, WithTraffic, WithTargetAnnotation("1")),
			hpa(testRevision, testNamespace, WithHPAClass, WithMetricAnnotation("cpu")),
		},
		Key: key(testRevision, testNamespace),
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: hpa(testRevision, testNamespace, WithHPAClass, WithTargetAnnotation("1"), WithMetricAnnotation("cpu")),
		}},
	}}

	table.Test(t, MakeFactory(func(listers *Listers, opt reconciler.Options) controller.Reconciler {
		return &Reconciler{
			Base:      reconciler.NewBase(opt, controllerAgentName),
			paLister:  listers.GetPodAutoscalerLister(),
			hpaLister: listers.GetHorizontalPodAutoscalerLister(),
		}
	}))
}

func key(name, namespace string) string {
	return namespace + "/" + name
}

func pa(name, namespace string, options ...PodAutoscalerOption) *autoscalingv1alpha1.PodAutoscaler {
	pa := &autoscalingv1alpha1.PodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: autoscalingv1alpha1.PodAutoscalerSpec{
			ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       name + "-deployment",
			},
			ServiceName: name + "-service",
		},
	}
	for _, opt := range options {
		opt(pa)
	}
	return pa
}

func hpa(name, namespace string, options ...PodAutoscalerOption) *autoscalingv1.HorizontalPodAutoscaler {
	return resources.MakeHPA(pa(name, namespace, options...))
}
