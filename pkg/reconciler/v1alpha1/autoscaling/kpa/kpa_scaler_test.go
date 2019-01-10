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

package kpa

import (
	"strconv"
	"testing"
	"time"

	"github.com/knative/pkg/apis"
	. "github.com/knative/pkg/logging/testing"
	"github.com/knative/serving/pkg/apis/autoscaling"
	pav1alpha1 "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	fakeKna "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	_ "github.com/knative/serving/pkg/system/testing"
	revisionresources "github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources/names"
	v1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	scalefake "k8s.io/client-go/scale/fake"
	clientgotesting "k8s.io/client-go/testing"
)

const (
	testNamespace = "test-namespace"
	testRevision  = "test-revision"
)

func TestKPAScaler(t *testing.T) {
	examples := []struct {
		label         string
		startReplicas int
		scaleTo       int32
		minScale      int32
		maxScale      int32
		wantReplicas  int
		wantScaling   bool
		kpaMutation   func(*pav1alpha1.PodAutoscaler)
	}{{
		label:         "waits to scale to zero (just before idle period)",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  1,
		wantScaling:   false,
		kpaMutation: func(k *pav1alpha1.PodAutoscaler) {
			kpaMarkActive(k, time.Now().Add(-stableWindow).Add(1*time.Second))
		},
	}, {
		label:         "waits to scale to zero after idle period",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  1,
		wantScaling:   false,
		kpaMutation: func(k *pav1alpha1.PodAutoscaler) {
			kpaMarkActive(k, time.Now().Add(-stableWindow))
		},
	}, {
		label:         "waits to scale to zero (just before grace period)",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  1,
		wantScaling:   false,
		kpaMutation: func(k *pav1alpha1.PodAutoscaler) {
			kpaMarkInactive(k, time.Now().Add(-gracePeriod).Add(1*time.Second))
		},
	}, {
		label:         "scale to zero after grace period",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  0,
		wantScaling:   true,
		kpaMutation: func(k *pav1alpha1.PodAutoscaler) {
			kpaMarkInactive(k, time.Now().Add(-gracePeriod))
		},
	}, {
		label:         "does not scale while activating",
		startReplicas: 1,
		scaleTo:       0,
		wantReplicas:  1,
		wantScaling:   false,
		kpaMutation: func(k *pav1alpha1.PodAutoscaler) {
			k.Status.MarkActivating("", "")
		},
	}, {
		label:         "scale down to minScale after grace period",
		startReplicas: 10,
		scaleTo:       0,
		minScale:      2,
		wantReplicas:  2,
		wantScaling:   true,
		kpaMutation: func(k *pav1alpha1.PodAutoscaler) {
			kpaMarkActive(k, time.Now().Add(-stableWindow))
		},
	}, {
		label:         "scales up",
		startReplicas: 1,
		scaleTo:       10,
		wantReplicas:  10,
		wantScaling:   true,
	}, {
		label:         "scales up to maxScale",
		startReplicas: 1,
		scaleTo:       10,
		maxScale:      8,
		wantReplicas:  8,
		wantScaling:   true,
	}, {
		label:         "scale up inactive revision",
		startReplicas: 0,
		scaleTo:       10,
		wantReplicas:  10,
		wantScaling:   true,
	}, {
		label:         "scales up from zero with no metrics",
		startReplicas: 0,
		scaleTo:       -1, // no metrics
		wantReplicas:  1,
		wantScaling:   true,
	}, {
		label:         "scales up from zero to desired one",
		startReplicas: 0,
		scaleTo:       1,
		wantReplicas:  1,
		wantScaling:   true,
	}, {
		label:         "scales up from zero to desired high scale",
		startReplicas: 0,
		scaleTo:       10,
		wantReplicas:  10,
		wantScaling:   true,
	}, {
		label:         "ignore negative scale",
		startReplicas: 12,
		scaleTo:       -1,
		wantReplicas:  12,
		wantScaling:   false,
	}}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			// The clients for our testing.
			servingClient := fakeKna.NewSimpleClientset()
			scaleClient := &scalefake.FakeScaleClient{}

			revision := newRevision(t, servingClient, e.minScale, e.maxScale)
			deployment := newDeployment(t, scaleClient, revision, e.startReplicas)
			revisionScaler := NewKPAScaler(servingClient, scaleClient, TestLogger(t), newConfigWatcher())

			pa := newKPA(t, servingClient, revision)
			if e.kpaMutation != nil {
				e.kpaMutation(pa)
			}

			revisionScaler.Scale(TestContextWithLogger(t), pa, e.scaleTo)

			if e.wantScaling {
				checkReplicas(t, scaleClient, deployment, e.wantReplicas)
			} else {
				checkNoScaling(t, scaleClient)
			}
		})
	}
}

func newKPA(t *testing.T, servingClient clientset.Interface, revision *v1alpha1.Revision) *pav1alpha1.PodAutoscaler {
	pa := revisionresources.MakeKPA(revision)
	pa.Status.InitializeConditions()
	_, err := servingClient.AutoscalingV1alpha1().PodAutoscalers(testNamespace).Create(pa)
	if err != nil {
		t.Fatal("Failed to create PA.", err)
	}

	return pa
}

func newRevision(t *testing.T, servingClient clientset.Interface, minScale, maxScale int32) *v1alpha1.Revision {
	annotations := map[string]string{}
	if minScale > 0 {
		annotations[autoscaling.MinScaleAnnotationKey] = strconv.Itoa(int(minScale))
	}
	if maxScale > 0 {
		annotations[autoscaling.MaxScaleAnnotationKey] = strconv.Itoa(int(maxScale))
	}
	rev := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   testNamespace,
			Name:        testRevision,
			Annotations: annotations,
		},
		Spec: v1alpha1.RevisionSpec{
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

func kpaMarkActive(pa *pav1alpha1.PodAutoscaler, ltt time.Time) {
	pa.Status.MarkActive()

	// This works because the conditions are sorted alphabetically
	pa.Status.Conditions[0].LastTransitionTime = apis.VolatileTime{Inner: metav1.NewTime(ltt)}
}

func kpaMarkInactive(pa *pav1alpha1.PodAutoscaler, ltt time.Time) {
	pa.Status.MarkInactive("", "")

	// This works because the conditions are sorted alphabetically
	pa.Status.Conditions[0].LastTransitionTime = apis.VolatileTime{Inner: metav1.NewTime(ltt)}
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
