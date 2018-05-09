package activator

import (
	"testing"

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	fakeEla "github.com/elafros/elafros/pkg/client/clientset/versioned/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakeK8s "k8s.io/client-go/kubernetes/fake"
)

func TestActivate_AlreadyActive(t *testing.T) {
	fakeElaClient := fakeEla.NewSimpleClientset()
	fakeK8sClient := fakeK8s.NewSimpleClientset()
	nsObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-namespace",
			Namespace: "",
		},
	}
	fakeK8sClient.CoreV1().Namespaces().Create(nsObj)
	fakeElaClient.ElafrosV1alpha1().Revisions("test-namespace").Create(newRevisionBuilder().build())
	fakeK8sClient.CoreV1().Endpoints("test-namespace").Create(newEndpointBuilder().build())
	a := NewRevisionActivator(fakeK8sClient, fakeElaClient)

	got, status, err := a.ActiveEndpoint("test-namespace", "test-rev")

	want := Endpoint{"ip", 8080}
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

type revisionBuilder struct {
	revision *v1alpha1.Revision
}

func newRevisionBuilder() *revisionBuilder {
	return &revisionBuilder{
		revision: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-rev",
				Namespace: "test-namespace",
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
	var status corev1.ConditionStatus
	if ready {
		status = corev1.ConditionTrue
	} else {
		status = corev1.ConditionFalse
	}
	new := &v1alpha1.RevisionCondition{
		Type:   v1alpha1.RevisionConditionReady,
		Status: status,
	}
	b.revision.Status.SetCondition(new)
	return b
}

type endpointBuilder struct {
	endpoint *corev1.Endpoints
}

func newEndpointBuilder() *endpointBuilder {
	return &endpointBuilder{
		endpoint: &corev1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-rev-service",
				Namespace: "test-namespace",
			},
			Subsets: []corev1.EndpointSubset{
				corev1.EndpointSubset{
					Addresses: []corev1.EndpointAddress{
						corev1.EndpointAddress{IP: "ip"},
					},
					Ports: []corev1.EndpointPort{
						corev1.EndpointPort{Port: int32(8080)},
					},
				},
			},
		},
	}
}

func (b *endpointBuilder) build() *corev1.Endpoints {
	return b.endpoint
}

func (b *endpointBuilder) withAddressPort(address string, port int32) *endpointBuilder {
	b.endpoint.Subsets[0].Addresses[0].IP = address
	b.endpoint.Subsets[0].Ports[0].Port = port
	return b
}
