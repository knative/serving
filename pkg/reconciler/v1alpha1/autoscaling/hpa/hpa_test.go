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
	"context"
	"fmt"
	"testing"
	"time"

	. "github.com/knative/pkg/logging/testing"
	"github.com/knative/serving/pkg/apis/autoscaling"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	fakeKna "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/reconciler"
	revisionresources "github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	fakeK8s "k8s.io/client-go/kubernetes/fake"
)

const (
	testNamespace = "test-namespace"
	testRevision  = "test-revision"
)

func TestReconcile(t *testing.T) {
	kubeClient := fakeK8s.NewSimpleClientset()
	servingClient := fakeKna.NewSimpleClientset()

	opts := reconciler.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           TestLogger(t),
	}

	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)
	kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)

	ctl := NewController(&opts,
		servingInformer.Autoscaling().V1alpha1().PodAutoscalers(),
		kubeInformer.Autoscaling().V1().HorizontalPodAutoscalers(),
	)

	rev := newTestRevision(testNamespace, testRevision)
	servingClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	servingInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)
	kpa := revisionresources.MakeKPA(rev)
	servingClient.AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	servingInformer.Autoscaling().V1alpha1().PodAutoscalers().Informer().GetIndexer().Add(kpa)
	reconcileDone := make(chan struct{})
	go func() {
		defer close(reconcileDone)
		err := ctl.Reconciler.Reconcile(context.TODO(), testNamespace+"/"+testRevision)
		if err != nil {
			t.Errorf("Reconcile() = %v", err)
		}
	}()

	// Wait for reconcile to finish
	select {
	case <-reconcileDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Reconciliation timed out")
	}

	// Verify HPA was created
	if _, err := kubeClient.AutoscalingV1().HorizontalPodAutoscalers(testNamespace).Get(testRevision, metav1.GetOptions{}); err != nil {
		t.Errorf("Wanted HPA. Got %v", err)
	}
}

func newTestRevision(namespace string, name string) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  fmt.Sprintf("/apis/ela/v1alpha1/namespaces/%s/revisions/%s", namespace, name),
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				autoscaling.ClassAnnotationKey: autoscaling.HPA,
			},
		},
		Spec: v1alpha1.RevisionSpec{
			Container: corev1.Container{
				Image:      "gcr.io/repo/image",
				Command:    []string{"echo"},
				Args:       []string{"hello", "world"},
				WorkingDir: "/tmp",
			},
			ConcurrencyModel: v1alpha1.RevisionRequestConcurrencyModelSingle,
		},
	}
}
