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
package activator

import (
	"net/http"
	"testing"
	"time"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	fakeKna "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	. "github.com/knative/pkg/logging/testing"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	fakeK8s "k8s.io/client-go/kubernetes/fake"
)

const (
	testNamespace   = "test-namespace"
	testRevision    = "test-rev"
	testService     = testRevision + "-service"
	testServiceFQDN = testService + "." + testNamespace + ".svc.cluster.local"
)

func TestActiveEndpoint_Active_StaysActive(t *testing.T) {
	k8s, kna := fakeClients()
	kna.ServingV1alpha1().Revisions(testNamespace).Create(newRevisionBuilder().build())
	k8s.CoreV1().Services(testNamespace).Create(newServiceBuilder().build())
	a := NewRevisionActivator(k8s, kna, TestLogger(t))

	got, status, err := a.ActiveEndpoint(testNamespace, testRevision)

	want := Endpoint{testServiceFQDN, 8080}
	if got != want {
		t.Errorf("Wrong endpoint. Want %+v. Got %+v.", want, got)
	}
	if status != Status(0) {
		t.Errorf("Unexpected error status. Want 0. Got %v.", status)
	}
	if err != nil {
		t.Errorf("Unexpected error. Want nil. Got %v.", err)
	}
}

func TestActiveEndpoint_Reserve_BecomesActive(t *testing.T) {
	k8s, kna := fakeClients()
	kna.ServingV1alpha1().Revisions(testNamespace).Create(
		newRevisionBuilder().
			withServingState(v1alpha1.RevisionServingStateReserve).
			build())
	k8s.CoreV1().Services(testNamespace).Create(newServiceBuilder().build())
	a := NewRevisionActivator(k8s, kna, TestLogger(t))

	got, status, err := a.ActiveEndpoint(testNamespace, testRevision)

	want := Endpoint{testServiceFQDN, 8080}
	if got != want {
		t.Errorf("Wrong endpoint. Want %+v. Got %+v.", want, got)
	}
	if status != Status(0) {
		t.Errorf("Unexpected error status. Want 0. Got %v.", status)
	}
	if err != nil {
		t.Errorf("Unexpected error. Want nil. Got %v.", err)
	}

	rev, _ := kna.ServingV1alpha1().Revisions(testNamespace).Get(testRevision, metav1.GetOptions{})
	if rev.Spec.ServingState != v1alpha1.RevisionServingStateActive {
		t.Errorf("Unexpected serving state. Want Active. Got %v.", rev.Spec.ServingState)
	}
}

func TestActiveEndpoint_Retired_StaysRetiredWithError(t *testing.T) {
	k8s, kna := fakeClients()
	kna.ServingV1alpha1().Revisions(testNamespace).Create(
		newRevisionBuilder().
			withServingState(v1alpha1.RevisionServingStateRetired).
			build())
	k8s.CoreV1().Services(testNamespace).Create(newServiceBuilder().build())
	a := NewRevisionActivator(k8s, kna, TestLogger(t))

	got, status, err := a.ActiveEndpoint(testNamespace, testRevision)

	want := Endpoint{}
	if got != want {
		t.Errorf("Wrong endpoint. Want %+v. Got %+v.", want, got)
	}
	if status != Status(http.StatusInternalServerError) {
		t.Errorf("Unexpected error status. Want %v. Got %v.", http.StatusInternalServerError, status)
	}
	if err == nil {
		t.Errorf("Expected error. Want error. Got nil.")
	}

	rev, _ := kna.ServingV1alpha1().Revisions(testNamespace).Get(testRevision, metav1.GetOptions{})
	if rev.Spec.ServingState != v1alpha1.RevisionServingStateRetired {
		t.Errorf("Unexpected serving state. Want Retired. Got %v.", rev.Spec.ServingState)
	}
}

func TestActiveEndpoint_Reserve_WaitsForReady(t *testing.T) {
	k8s, kna := fakeClients()
	kna.ServingV1alpha1().Revisions(testNamespace).Create(
		newRevisionBuilder().
			withServingState(v1alpha1.RevisionServingStateReserve).
			withReady(false).
			build())
	k8s.CoreV1().Services(testNamespace).Create(newServiceBuilder().build())
	a := NewRevisionActivator(k8s, kna, TestLogger(t))

	ch := make(chan activationResult)
	go func() {
		endpoint, status, err := a.ActiveEndpoint(testNamespace, testRevision)
		ch <- activationResult{endpoint, status, err}
	}()

	time.Sleep(100 * time.Millisecond)
	select {
	case <-ch:
		t.Errorf("Unexpected result before revision is ready.")
	default:
	}

	rev, _ := kna.ServingV1alpha1().Revisions(testNamespace).Get(testRevision, metav1.GetOptions{})
	rev.Status.MarkContainerHealthy()
	rev.Status.MarkResourcesAvailable()
	kna.ServingV1alpha1().Revisions(testNamespace).Update(rev)

	time.Sleep(3 * time.Second)
	select {
	case result := <-ch:
		want := Endpoint{testServiceFQDN, 8080}
		if result.endpoint != want {
			t.Errorf("Unexpected endpoint. Want %+v. Got %+v.", want, result.endpoint)
		}
		if result.status != Status(0) {
			t.Errorf("Unexpected error state. Want 0. Got %v.", result.status)
		}
		if result.err != nil {
			t.Errorf("Unexpected error. Want nil. Got %v.", result.err)
		}
	default:
		t.Errorf("Expected result after revision ready.")
	}
}

func TestActiveEndpoint_Reserve_ReadyTimeoutWithError(t *testing.T) {
	k8s, kna := fakeClients()
	kna.ServingV1alpha1().Revisions(testNamespace).Create(
		newRevisionBuilder().
			withServingState(v1alpha1.RevisionServingStateReserve).
			withReady(false).
			build())
	k8s.CoreV1().Services(testNamespace).Create(newServiceBuilder().build())
	a := NewRevisionActivator(k8s, kna, TestLogger(t))
	a.(*revisionActivator).readyTimout = 200 * time.Millisecond

	ch := make(chan activationResult)
	go func() {
		endpoint, status, err := a.ActiveEndpoint(testNamespace, testRevision)
		ch <- activationResult{endpoint, status, err}
	}()

	<-time.After(100 * time.Millisecond)
	select {
	case <-ch:
		t.Errorf("Unexpected result before revision is ready.")
	default:
	}

	time.Sleep(3 * time.Second)
	select {
	case result := <-ch:
		if got, want := result.endpoint, (Endpoint{}); got != want {
			t.Errorf("Unexpected endpoint. Want %+v. Got %+v.", want, got)
		}
		if got, want := result.status, Status(http.StatusInternalServerError); got != want {
			t.Errorf("Unexpected error state. Want %v. Got %v.", want, got)
		}
		if result.err == nil {
			t.Errorf("Expected error. Want error. Got nil.")
		}
	default:
		t.Errorf("Expected result after timeout.")
	}
}

func fakeClients() (kubernetes.Interface, clientset.Interface) {
	nsObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testNamespace,
			Namespace: "",
		},
	}
	return fakeK8s.NewSimpleClientset(nsObj), fakeKna.NewSimpleClientset()
}

type revisionBuilder struct {
	revision *v1alpha1.Revision
}

func newRevisionBuilder() *revisionBuilder {
	return &revisionBuilder{
		revision: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testRevision,
				Namespace: testNamespace,
			},
			Spec: v1alpha1.RevisionSpec{
				Container: corev1.Container{
					Image: "gcr.io/repo/image",
				},
				ServingState: v1alpha1.RevisionServingStateActive,
			},
			Status: v1alpha1.RevisionStatus{
				Conditions: []v1alpha1.RevisionCondition{
					v1alpha1.RevisionCondition{
						Type:   v1alpha1.RevisionConditionReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		},
	}
}

func (b *revisionBuilder) build() *v1alpha1.Revision {
	return b.revision
}

func (b *revisionBuilder) withServingState(servingState v1alpha1.RevisionServingStateType) *revisionBuilder {
	b.revision.Spec.ServingState = servingState
	return b
}

func (b *revisionBuilder) withReady(ready bool) *revisionBuilder {
	if ready {
		b.revision.Status.MarkContainerHealthy()
		b.revision.Status.MarkResourcesAvailable()
	} else {
		b.revision.Status.MarkContainerMissing("reasonz")
	}
	return b
}

type serviceBuilder struct {
	service *corev1.Service
}

func newServiceBuilder() *serviceBuilder {
	return &serviceBuilder{

		service: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testService,
				Namespace: testNamespace,
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name: "http",
						Port: 8080,
					},
				},
			},
		},
	}
}

func (b *serviceBuilder) build() *corev1.Service {
	return b.service
}
