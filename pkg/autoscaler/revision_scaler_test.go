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

package autoscaler_test

import (
	"testing"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	fakeKna "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources/names"
	"go.uber.org/zap"
	"k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	scalefake "k8s.io/client-go/scale/fake"
	clientgotesting "k8s.io/client-go/testing"
)

const (
	testNamespace   = "test-namespace"
	testRevision    = "test-revision"
	testRevisionKey = "test-namespace/test-revision"
)

// FIXME: Matt Moor asked 'Can we combine some of these tests into a "table test"?'

func TestRevisionScalerScalesToZero(t *testing.T) {
	revision := newRevision(v1alpha1.RevisionServingStateActive)
	deployment := newDeployment(revision, 1)

	revisionScaler, servingClient, _ := createRevisionScaler(t, revision, deployment)

	revisionScaler.Scale(revision, 0)

	checkServingState(t, servingClient, v1alpha1.RevisionServingStateReserve)
}

func TestRevisionScalerScalesUp(t *testing.T) {
	revision := newRevision(v1alpha1.RevisionServingStateActive)
	deployment := newDeployment(revision, 1)
	revisionScaler, servingClient, scaleClient := createRevisionScaler(t, revision, deployment)

	revisionScaler.Scale(revision, 10)

	checkServingState(t, servingClient, v1alpha1.RevisionServingStateActive)
	checkReplicas(t, scaleClient, deployment, 10)
}

func TestRevisionScalerDoesScaleUpInactiveRevision(t *testing.T) {
	revision := newRevision(v1alpha1.RevisionServingStateReserve)
	deployment := newDeployment(revision, 1)
	revisionScaler, servingClient, scaleClient := createRevisionScaler(t, revision, deployment)

	revisionScaler.Scale(revision, 10)

	checkServingState(t, servingClient, v1alpha1.RevisionServingStateReserve)
	checkNoScaling(t, scaleClient)
}

func TestRevisionScalerDoesNotScaleUpFromZero(t *testing.T) {
	revision := newRevision(v1alpha1.RevisionServingStateActive) // normally implies a non-zero scale
	deployment := newDeployment(revision, 0)
	revisionScaler, servingClient, scaleClient := createRevisionScaler(t, revision, deployment)

	revisionScaler.Scale(revision, 10)

	checkServingState(t, servingClient, v1alpha1.RevisionServingStateActive)
	checkNoScaling(t, scaleClient)
}

func createRevisionScaler(t *testing.T, revision *v1alpha1.Revision, deployment *v1.Deployment) (autoscaler.RevisionScaler, clientset.Interface, *scalefake.FakeScaleClient) {
	sf := &scalefake.FakeScaleClient{}
	servingClient := fakeKna.NewSimpleClientset()

	revisionScaler := autoscaler.NewRevisionScaler(servingClient, sf, zap.NewNop().Sugar())

	_, err := servingClient.ServingV1alpha1().Revisions(testNamespace).Create(revision)
	if err != nil {
		t.Fatal("Failed to get deployment.", err)
	}

	sf.AddReactor("get", "deployments", func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
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

	return revisionScaler, servingClient, sf
}

func newRevision(servingState v1alpha1.RevisionServingStateType) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRevision,
		},
		Spec: v1alpha1.RevisionSpec{
			ServingState: servingState,
		},
	}
}

func newDeployment(revision *v1alpha1.Revision, replicas int) *v1.Deployment {
	scale := int32(replicas)
	return &v1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      names.Deployment(revision),
		},
		Spec: v1.DeploymentSpec{
			Replicas: &scale,
		},
	}
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
