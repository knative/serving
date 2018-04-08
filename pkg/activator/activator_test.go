/*
Copyright 2018 Google Inc. All Rights Reserved.
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
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/elafros/elafros/pkg/apis/ela"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	fakeclientset "github.com/elafros/elafros/pkg/client/clientset/versioned/fake"
	"github.com/elafros/elafros/pkg/controller"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
)

const (
	testNamespace string = "default"
	testPort             = 1234
)

// FakeRoundTripper serves as a fake transport
type FakeRoundTripper struct{}

// RoundTrip returns a response with status 200.
func (rrt FakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	body := "everything works fine."
	return &http.Response{
		Status:        "200 OK",
		StatusCode:    200,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Body:          ioutil.NopCloser(bytes.NewBufferString(body)),
		ContentLength: int64(len(body)),
		Request:       req,
		Header:        make(http.Header, 0),
	}, nil
}

func getHTTPRequest(t *testing.T) *http.Request {
	req, err := http.NewRequest("GET", "/api/projects", nil)
	if err != nil {
		t.Error("Failed to create http request.")
	}
	return req
}

func getTestRevision(servingState v1alpha1.RevisionServingStateType) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/ela/v1alpha1/namespaces/test/revisions/test-rev",
			Name:      "test-rev",
			Namespace: testNamespace,
			Labels: map[string]string{
				"testLabel1":      "foo",
				"testLabel2":      "bar",
				ela.RouteLabelKey: "test-route",
			},
		},
		Spec: v1alpha1.RevisionSpec{
			Container: &corev1.Container{
				Image:      "gcr.io/repo/image",
				Command:    []string{"echo"},
				Args:       []string{"hello", "world"},
				WorkingDir: "/tmp",
				Env: []corev1.EnvVar{{
					Name:  "EDITOR",
					Value: "emacs",
				}},
				LivenessProbe: &corev1.Probe{
					TimeoutSeconds: 42,
				},
				ReadinessProbe: &corev1.Probe{
					TimeoutSeconds: 43,
				},
				TerminationMessagePath: "/dev/null",
			},
			ServingState: servingState,
		},
	}
}

func getActivator(t *testing.T, rev *v1alpha1.Revision) *Activator {
	// Create fake clients
	kubeClient := fakekubeclientset.NewSimpleClientset()
	elaClient := fakeclientset.NewSimpleClientset()
	if rev != nil {
		// Add the revision
		elaClient.ElafrosV1alpha1().Revisions(rev.GetNamespace()).Create(rev)
		// Add the k8s service
		kubeClient.CoreV1().Services(rev.GetNamespace()).Create(
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      controller.GetElaK8SServiceNameForRevision(rev),
					Namespace: rev.GetNamespace(),
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "abc",
					Ports: []corev1.ServicePort{
						{
							Name:       "test-port",
							Port:       int32(testPort),
							TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: testPort},
						},
					},
					Type: "NodePort",
				},
			},
		)
	}

	tripper := FakeRoundTripper{}
	a, err := NewActivator(kubeClient, elaClient, tripper)
	if err != nil {
		t.Fatal("Failed to create an activator!")
	}

	return a
}

func TestGetRevisionTargetURL(t *testing.T) {
	reservedRev := getTestRevision(v1alpha1.RevisionServingStateReserve)
	a := getActivator(t, reservedRev)
	targetURL, err := a.getRevisionTargetURL(reservedRev)
	if err != nil {
		t.Errorf("Error in getRevisionTargetURL %v", err)
	}
	expectedURL := "http://abc:1234"
	if targetURL != expectedURL {
		t.Errorf("getRevisionTargetURL returned unexpected url %s, expected %s", targetURL, expectedURL)
	}
}

func testHandler_revision(t *testing.T, servingState v1alpha1.RevisionServingStateType, expectedStatus int) {
	rev := getTestRevision(servingState)
	a := getActivator(t, rev)

	req := getHTTPRequest(t)
	// response recorder to record the response
	responseRecorder := httptest.NewRecorder()
	handler := http.HandlerFunc(a.handler)
	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	handler.ServeHTTP(responseRecorder, req)

	// Check the status code is what we expect.
	if status := responseRecorder.Code; status != expectedStatus {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, expectedStatus)
	}
}

// Test for a revision with reserve status.
func TestHandler_reserveRevision(t *testing.T) {
	testHandler_revision(t, v1alpha1.RevisionServingStateReserve, http.StatusOK)
}

// Test for a revision with active status.
func TestHandler_activeRevision(t *testing.T) {
	testHandler_revision(t, v1alpha1.RevisionServingStateActive, http.StatusOK)
}

// Test for a revision with reretired status.
func TestHandler_retiredRevision(t *testing.T) {
	testHandler_revision(t, v1alpha1.RevisionServingStateRetired, http.StatusServiceUnavailable)
}

// Test for a revision with unknown status.
func TestHandler_unknowRevision(t *testing.T) {
	testHandler_revision(t, "Unknown", http.StatusServiceUnavailable)
}
