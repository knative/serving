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
	testNamespace     = "test-namespace"
	testConfiguration = "test-configuration"
	testRevision      = "test-rev"
	testService       = testRevision + "-service"
	testServiceFQDN   = testService + "." + testNamespace + ".svc.cluster.local"
)

var defaultRevisionLabels map[string]string

func init() {
	defaultRevisionLabels = map[string]string{
		serving.ServiceLabelKey:       "test-service",
		serving.ConfigurationLabelKey: "test-config",
	}
}

type mockReporter struct{}

func (r *mockReporter) ReportRequest(ns, service, config, rev, servingState string, v float64) error {
	return nil
}

func (r *mockReporter) ReportResponseCount(ns, service, config, rev string, responseCode, numTries int, v float64) error {
	return nil
}

func (r *mockReporter) ReportResponseTime(ns, service, config, rev string, responseCode int, d time.Duration) error {
	return nil
}

func TestActiveEndpoint_Reserve_WaitsForReady(t *testing.T) {
	k8s, kna := fakeClients()
	kna.ServingV1alpha1().Revisions(testNamespace).Create(
		newRevisionBuilder(defaultRevisionLabels).
			withServingState(v1alpha1.RevisionServingStateReserve).
			withReady(false).
			build())
	k8s.CoreV1().Services(testNamespace).Create(newServiceBuilder().build())
	a := NewRevisionActivator(k8s, kna, TestLogger(t), &mockReporter{})

	ch := make(chan ActivationResult)
	go func() {
		ch <- a.ActiveEndpoint(testNamespace, testRevision)
	}()

	time.Sleep(100 * time.Millisecond)
	select {
	case <-ch:
		t.Errorf("Unexpected result before revision is ready.")
	default:
	}

	rev, _ := kna.ServingV1alpha1().Revisions(testNamespace).Get(testRevision, metav1.GetOptions{})
	rev.Status.MarkActive()
	rev.Status.MarkContainerHealthy()
	rev.Status.MarkResourcesAvailable()
	kna.ServingV1alpha1().Revisions(testNamespace).Update(rev)

	time.Sleep(3 * time.Second)
	select {
	case ar := <-ch:
		want := Endpoint{testServiceFQDN, 8080}
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
	default:
		t.Errorf("Expected result after revision ready.")
	}
}

func TestActiveEndpoint_Reserve_ReadyTimeoutWithError(t *testing.T) {
	k8s, kna := fakeClients()
	kna.ServingV1alpha1().Revisions(testNamespace).Create(
		newRevisionBuilder(defaultRevisionLabels).
			withServingState(v1alpha1.RevisionServingStateReserve).
			withReady(false).
			build())
	k8s.CoreV1().Services(testNamespace).Create(newServiceBuilder().build())
	a := NewRevisionActivator(k8s, kna, TestLogger(t), &mockReporter{})
	a.(*revisionActivator).readyTimout = 200 * time.Millisecond

	ch := make(chan ActivationResult)
	go func() {
		ch <- a.ActiveEndpoint(testNamespace, testRevision)
	}()

	<-time.After(100 * time.Millisecond)
	select {
	case <-ch:
		t.Errorf("Unexpected result before revision is ready.")
	default:
	}

	time.Sleep(3 * time.Second)
	select {
	case ar := <-ch:
		if got, want := ar.Endpoint, (Endpoint{}); got != want {
			t.Errorf("Unexpected endpoint. Want %+v. Got %+v.", want, got)
		}
		if got, want := ar.Status, http.StatusInternalServerError; got != want {
			t.Errorf("Unexpected error state. Want %v. Got %v.", want, got)
		}
		if ar.ServiceName != "" {
			t.Errorf("Unexpected service name. Want empty. Got %v.", ar.ServiceName)
		}
		if ar.ConfigurationName != "" {
			t.Errorf("Unexpected configuration name. Want empty. Got %v.", ar.ConfigurationName)
		}
		if ar.Error == nil {
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
				},
				ServingState: v1alpha1.RevisionServingStateActive,
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
				Ports: []corev1.ServicePort{{
					Name: revisionresources.ServicePortName,
					Port: 8080,
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
