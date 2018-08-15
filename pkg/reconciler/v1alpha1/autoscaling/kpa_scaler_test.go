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

package autoscaling_test

import (
	"testing"

	kpa "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	fakeKna "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/autoscaling"
	revisionresources "github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources/names"
	"k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	scalefake "k8s.io/client-go/scale/fake"
	clientgotesting "k8s.io/client-go/testing"

	. "github.com/knative/pkg/logging/testing"
)

const (
	testNamespace = "test-namespace"
	testRevision  = "test-revision"
)

func TestKPAScaler(t *testing.T) {
	examples := []struct {
		label         string
		startState    v1alpha1.RevisionServingStateType
		startReplicas int
		scaleTo       int32
		wantState     v1alpha1.RevisionServingStateType
		wantReplicas  int
		wantScaling   bool
	}{{
		label:         "scales to zero",
		startState:    v1alpha1.RevisionServingStateActive,
		startReplicas: 1,
		scaleTo:       0,
		wantState:     v1alpha1.RevisionServingStateReserve,
		wantReplicas:  1,
		wantScaling:   false,
	}, {
		label:         "scales up",
		startState:    v1alpha1.RevisionServingStateActive,
		startReplicas: 1,
		scaleTo:       10,
		wantState:     v1alpha1.RevisionServingStateActive,
		wantReplicas:  10,
		wantScaling:   true,
	}, {
		label:         "scales up inactive revision",
		startState:    v1alpha1.RevisionServingStateReserve,
		startReplicas: 1,
		scaleTo:       10,
		wantState:     v1alpha1.RevisionServingStateReserve,
		wantReplicas:  1,
		wantScaling:   false,
	}, {
		label:         "does not scale up from zero",
		startState:    v1alpha1.RevisionServingStateActive,
		startReplicas: 0,
		scaleTo:       10,
		wantState:     v1alpha1.RevisionServingStateActive,
		wantReplicas:  0,
		wantScaling:   false,
	}}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			// The clients for our testing.
			servingClient := fakeKna.NewSimpleClientset()
			scaleClient := &scalefake.FakeScaleClient{}

			revision := newRevision(t, servingClient, e.startState)
			deployment := newDeployment(t, scaleClient, revision, e.startReplicas)
			revisionScaler := autoscaling.NewKPAScaler(servingClient, scaleClient, TestLogger(t))

			kpa := newKPA(t, servingClient, revision)
			revisionScaler.Scale(kpa, e.scaleTo)

			checkServingState(t, servingClient, e.wantState)

			if e.wantScaling {
				checkReplicas(t, scaleClient, deployment, e.wantReplicas)
			} else {
				checkNoScaling(t, scaleClient)
			}
		})
	}
}

func newKPA(t *testing.T, servingClient clientset.Interface, revision *v1alpha1.Revision) *kpa.PodAutoscaler {
	kpa := revisionresources.MakeKPA(revision)
	_, err := servingClient.AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(kpa)
	if err != nil {
		t.Fatal("Failed to create KPA.", err)
	}

	return kpa
}

func newRevision(t *testing.T, servingClient clientset.Interface, servingState v1alpha1.RevisionServingStateType) *v1alpha1.Revision {
	rev := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRevision,
		},
		Spec: v1alpha1.RevisionSpec{
			ServingState:     servingState,
			ConcurrencyModel: "Multi",
		},
	}
	rev, err := servingClient.ServingV1alpha1().Revisions(testNamespace).Create(rev)
	if err != nil {
		t.Fatal("Failed to create revision.", err)
	}

	return rev
}

func newDeployment(t *testing.T, scaleClient *scalefake.FakeScaleClient, revision *v1alpha1.Revision, replicas int) *v1.Deployment {
	scale := int32(replicas)
	deployment := &v1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      names.Deployment(revision),
		},
		Spec: v1.DeploymentSpec{
			Replicas: &scale,
		},
		Status: v1.DeploymentStatus{
			Replicas: scale,
		},
	}

	scaleClient.AddReactor("get", "deployments", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
		obj := &autoscalingv1.Scale{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			},
			Spec: autoscalingv1.ScaleSpec{
				Replicas: *deployment.Spec.Replicas,
			},
			Status: autoscalingv1.ScaleStatus{
				Replicas: deployment.Status.Replicas,
			},
		}
		return true, obj, nil
	})

	return deployment
}

func checkServingState(t *testing.T, servingClient clientset.Interface, servingState v1alpha1.RevisionServingStateType) {
	t.Helper()

	updatedRev, err := servingClient.ServingV1alpha1().Revisions(testNamespace).Get(testRevision, metav1.GetOptions{})
	if err != nil {
		t.Fatal("Failed to get revision.", err)
	}

	if updatedRev.Spec.ServingState != servingState {
		t.Fatal("Unexpected revision serving state.", updatedRev.Spec.ServingState)
	}
}

func checkReplicas(t *testing.T, scaleClient *scalefake.FakeScaleClient, deployment *v1.Deployment, expectedScale int) {
	t.Helper()

	found := false
	for _, action := range scaleClient.Actions() {
		switch action.GetVerb() {
		case "update":
			scl := action.(clientgotesting.UpdateAction).GetObject().(*autoscalingv1.Scale)
			if scl.Name != deployment.Name {
				continue
			}
			if got, want := scl.Spec.Replicas, int32(expectedScale); got != want {
				t.Errorf("Replicas = %v, wanted %v", got, want)
			}
			found = true
		}
	}

	if !found {
		t.Errorf("Did not see scale update for %v", deployment.Name)
	}
}

func checkNoScaling(t *testing.T, scaleClient *scalefake.FakeScaleClient) {
	t.Helper()

	for _, action := range scaleClient.Actions() {
		switch action.GetVerb() {
		case "update":
			t.Errorf("Unexpected update: %v", action)
		}
	}
}
