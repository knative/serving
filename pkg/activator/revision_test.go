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

	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	. "github.com/knative/pkg/logging/testing"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	fakeKna "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	revisionresources "github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources"
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

var defaultRevisionLabels = map[string]string{
	serving.ServiceLabelKey:       "test-service",
	serving.ConfigurationLabelKey: "test-config",
}

func TestActiveEndpoint_Reserve_WaitsForReady(t *testing.T) {
	k8s, kna := fakeClients()
	kna.ServingV1alpha1().Revisions(testNamespace).Create(
		newRevisionBuilder(defaultRevisionLabels).
			withReady(false).
			build())
	k8s.CoreV1().Services(testNamespace).Create(newServiceBuilder().build())
	a := NewRevisionActivator(k8s, kna, TestLogger(t))

	ch := make(chan ActivationResult)
	go func() {
		ch <- a.ActiveEndpoint(testNamespace, testRevision)
	}()

	select {
	case <-ch:
		t.Error("Unexpected result before revision is ready.")
	case <-time.After(1200 * time.Millisecond):
		// Wait long enough, so that ActiveEndpoint Go routine sets up the Watch.
		// It does not fire any events internally, so we have to "sleep".
	}

	rev, _ := kna.ServingV1alpha1().Revisions(testNamespace).Get(testRevision, metav1.GetOptions{})
	rev.Status.MarkActive()
	rev.Status.MarkContainerHealthy()
	rev.Status.MarkResourcesAvailable()
	kna.ServingV1alpha1().Revisions(testNamespace).Update(rev)

	select {
	case ar := <-ch:
		want := Endpoint{testServiceFQDN, v1alpha1.DefaultUserPort}
		if ar.Endpoint != want {
			t.Errorf("Unexpected endpoint. Want %+v. Got %+v.", want, ar.Endpoint)
		}
		if ar.Status != http.StatusOK {
			t.Errorf("Unexpected error state. Want 0. Got %v.", ar.Status)
		}
		if ar.ServiceName != "test-service" {
			t.Errorf("Unexpected service name. Want test-service. Got %v.", ar.ServiceName)
		}
		if ar.ConfigurationName != "test-config" {
			t.Errorf("Unexpected configuration name. Want test-config. Got %v.", ar.ConfigurationName)
		}
		if ar.Error != nil {
			t.Errorf("Unexpected error. Want nil. Got %v.", ar.Error)
		}
	case <-time.After(3 * time.Second):
		t.Error("Expected result after revision ready.")
	}
}

func TestActiveEndpoint_Reserve_WaitsForReady2Step(t *testing.T) {
	// This generates two updates, so that the inner loop of the `Watch` can be exercised.
	k8s, kna := fakeClients()
	kna.ServingV1alpha1().Revisions(testNamespace).Create(
		newRevisionBuilder(defaultRevisionLabels).
			withReady(false).
			build())
	k8s.CoreV1().Services(testNamespace).Create(newServiceBuilder().build())
	a := NewRevisionActivator(k8s, kna, TestLogger(t))

	ch := make(chan ActivationResult)
	go func() {
		ch <- a.ActiveEndpoint(testNamespace, testRevision)
	}()

	select {
	case <-ch:
		t.Error("Unexpected result before revision is ready.")
	case <-time.After(1000 * time.Millisecond):
		// Wait long enough, so that `ActiveEndpoint()` Go routine above
		// sets up the `Watch` on the revisions. We need to sleep long enough
		// for this to happen reliably, otherwise the test will flake.
	}

	// Partially update the service, to trigger a watch,
	// which would not finish the loop.
	rev, _ := kna.ServingV1alpha1().Revisions(testNamespace).Get(testRevision, metav1.GetOptions{})
	rev.Status.MarkResourcesAvailable()
	kna.ServingV1alpha1().Revisions(testNamespace).Update(rev)

	// ... and then finally make revision ready after a timeout.
	go func() {
		time.Sleep(250 * time.Millisecond)
		rev, _ := kna.ServingV1alpha1().Revisions(testNamespace).Get(testRevision, metav1.GetOptions{})
		rev.Status.MarkActive()
		rev.Status.MarkContainerHealthy()
		kna.ServingV1alpha1().Revisions(testNamespace).Update(rev)
	}()

	select {
	case ar := <-ch:
		want := Endpoint{testServiceFQDN, v1alpha1.DefaultUserPort}
		if ar.Endpoint != want {
			t.Errorf("Unexpected endpoint. Want %+v. Got %+v.", want, ar.Endpoint)
		}
		if ar.Status != http.StatusOK {
			t.Errorf("Unexpected error state. Want 0. Got %v.", ar.Status)
		}
		if ar.ServiceName != "test-service" {
			t.Errorf("Unexpected service name. Want test-service. Got %v.", ar.ServiceName)
		}
		if ar.ConfigurationName != "test-config" {
			t.Errorf("Unexpected configuration name. Want test-config. Got %v.", ar.ConfigurationName)
		}
		if ar.Error != nil {
			t.Errorf("Unexpected error. Want nil. Got %v.", ar.Error)
		}
	case <-time.After(3 * time.Second):
		t.Error("Expected result after revision ready.")
	}
}

func TestActiveEndpoint_Reserve_AlreadyReady(t *testing.T) {
	k8s, kna := fakeClients()
	kna.ServingV1alpha1().Revisions(testNamespace).Create(
		newRevisionBuilder(defaultRevisionLabels).
			withReady(false).
			build())
	k8s.CoreV1().Services(testNamespace).Create(newServiceBuilder().build())
	a := NewRevisionActivator(k8s, kna, TestLogger(t))

	rev, _ := kna.ServingV1alpha1().Revisions(testNamespace).Get(testRevision, metav1.GetOptions{})
	rev.Status.MarkActive()
	rev.Status.MarkContainerHealthy()
	rev.Status.MarkResourcesAvailable()
	_, err := kna.ServingV1alpha1().Revisions(testNamespace).Update(rev)
	if err != nil {
		t.Fatalf("Error updating the revision %s: %v", testRevision, err)
	}

	ch := make(chan ActivationResult)
	go func() {
		ch <- a.ActiveEndpoint(testNamespace, testRevision)
	}()

	select {
	case ar := <-ch:
		want := Endpoint{testServiceFQDN, v1alpha1.DefaultUserPort}
		if ar.Endpoint != want {
			t.Errorf("Unexpected endpoint. Want %+v. Got %+v.", want, ar.Endpoint)
		}
		if ar.Status != http.StatusOK {
			t.Errorf("Unexpected error state. Want 0. Got %v.", ar.Status)
		}
		if ar.ServiceName != "test-service" {
			t.Errorf("Unexpected service name. Want test-service. Got %v.", ar.ServiceName)
		}
		if ar.ConfigurationName != "test-config" {
			t.Errorf("Unexpected configuration name. Want test-config. Got %v.", ar.ConfigurationName)
		}
		if ar.Error != nil {
			t.Errorf("Unexpected error. Want nil. Got %v.", ar.Error)
		}
	case <-time.After(3 * time.Second):
		t.Error("Expected result after revision ready @", time.Now())
	}
}

func TestActivateEndpoint_NotFound(t *testing.T) {
	// Tests when revision can't be found.
	k8s, kna := fakeClients()
	kna.ServingV1alpha1().Revisions(testNamespace).Create(
		newRevisionBuilder(defaultRevisionLabels).
			withReady(false).
			build())
	k8s.CoreV1().Services(testNamespace).Create(newServiceBuilder().build())
	a := NewRevisionActivator(k8s, kna, TestLogger(t))

	ae := a.ActiveEndpoint(testNamespace, testRevision+"dne")
	if got, want := ae.Status, http.StatusInternalServerError; got != want {
		t.Errorf("NotFound revision status: got: %v, want: %v", got, want)
	}

}

func TestActiveEndpoint_Reserve_ReadyTimeoutWithError(t *testing.T) {
	k8s, kna := fakeClients()
	kna.ServingV1alpha1().Revisions(testNamespace).Create(
		newRevisionBuilder(defaultRevisionLabels).
			withReady(false).
			build())
	k8s.CoreV1().Services(testNamespace).Create(newServiceBuilder().build())
	a := NewRevisionActivator(k8s, kna, TestLogger(t))
	a.(*revisionActivator).readyTimout = 200 * time.Millisecond

	ch := make(chan ActivationResult)
	go func() {
		ch <- a.ActiveEndpoint(testNamespace, testRevision)
	}()

	select {
	case <-ch:
		t.Error("Unexpected result before revision is ready.")
	case <-time.After(100 * time.Millisecond):
		break
	}

	select {
	case ar := <-ch:
		if got, want := ar.Endpoint, (Endpoint{}); got != want {
			t.Errorf("Unexpected endpoint = %+v, want: %+v.", got, want)
		}
		if got, want := ar.Status, http.StatusInternalServerError; got != want {
			t.Errorf("Unexpected error state = %v, want: %v.", got, want)
		}
		if ar.ServiceName != "" {
			t.Errorf("ServiceName = %s, expected empty.", ar.ServiceName)
		}
		if ar.ConfigurationName != "" {
			t.Errorf("ConfigurationName = %s, expected empty.", ar.ConfigurationName)
		}
		if ar.Error == nil {
			t.Error("Expected error.")
		}
	case <-time.After(3 * time.Second):
		t.Error("Expected result after timeout.")
	}
}

func fakeClients() (kubernetes.Interface, clientset.Interface) {
	nsObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	return fakeK8s.NewSimpleClientset(nsObj), fakeKna.NewSimpleClientset()
}

type revisionBuilder struct {
	revision *v1alpha1.Revision
}

func newRevisionBuilder(labels map[string]string) *revisionBuilder {
	return &revisionBuilder{
		revision: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testRevision,
				Namespace: testNamespace,
				Labels:    labels,
			},
			Spec: v1alpha1.RevisionSpec{
				Container: corev1.Container{
					Image: "gcr.io/repo/image",
					Ports: []corev1.ContainerPort{{Name: "h2c"}},
				},
			},
			Status: v1alpha1.RevisionStatus{
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RevisionConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
	}
}

func (b *revisionBuilder) build() *v1alpha1.Revision {
	return b.revision
}

func (b *revisionBuilder) withRevisionName(name string) *revisionBuilder {
	b.revision.ObjectMeta.Name = name
	return b
}

func (b *revisionBuilder) withReady(ready bool) *revisionBuilder {
	if ready {
		b.revision.Status.MarkContainerHealthy()
		b.revision.Status.MarkResourcesAvailable()
		b.revision.Status.MarkActive()
	} else {
		b.revision.Status.MarkInactive("reasonz", "detailz")
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
				Ports: []corev1.ServicePort{{
					Name: revisionresources.ServicePortNameH2C,
					Port: v1alpha1.DefaultUserPort,
				}, {
					Name: "anotherport",
					Port: 9090,
				}},
			},
		},
	}
}

func (b *serviceBuilder) build() *corev1.Service {
	return b.service
}
