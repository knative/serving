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
	"time"

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
	testNamespace    string = "default"
	testRevisionName string = "test-rev"
	testPort                = 1234
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

func getHTTPRequest(t *testing.T, revisionName string) *http.Request {
	req, err := http.NewRequest("GET", "/api/projects", nil)
	if err != nil {
		t.Error("Failed to create http request.")
	}
	req.Header.Set("Elafros-Revision", revisionName)
	return req
}

func getTestRevision(servingState v1alpha1.RevisionServingStateType, revName string) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/ela/v1alpha1/namespaces/test/revisions/test-rev",
			Name:      revName,
			Namespace: testNamespace,
			Labels: map[string]string{
				"testLabel1":      "foo",
				"testLabel2":      "bar",
				ela.RouteLabelKey: "test-route",
			},
		},
		Spec: v1alpha1.RevisionSpec{
			Container: corev1.Container{
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

// Add the test revisions to the environment.
func addTestRevisions(t *testing.T, kubeClient *fakekubeclientset.Clientset, elaClient *fakeclientset.Clientset, revisions ...*v1alpha1.Revision) {
	for _, rev := range revisions {
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
	}
}

func getActivator(t *testing.T, kubeClient *fakekubeclientset.Clientset, elaClient *fakeclientset.Clientset) *Activator {
	tripper := FakeRoundTripper{}
	a, err := NewActivator(kubeClient, elaClient, tripper)
	if err != nil {
		t.Fatal("Failed to create an activator!")
	}
	return a
}

func TestGetRevisionTargetURL(t *testing.T) {
	reservedRev := getTestRevision(v1alpha1.RevisionServingStateReserve, testRevisionName)
	elaClient := fakeclientset.NewSimpleClientset()
	kubeClient := fakekubeclientset.NewSimpleClientset()
	addTestRevisions(t, kubeClient, elaClient, reservedRev)
	a := getActivator(t, kubeClient, elaClient)
	targetURL, err := a.getRevisionTargetURL(reservedRev)
	if err != nil {
		t.Errorf("Error in getRevisionTargetURL %v", err)
	}
	expectedURL := "http://abc:1234"
	if targetURL != expectedURL {
		t.Errorf("getRevisionTargetURL returned unexpected url %s, expected %s", targetURL, expectedURL)
	}
}

func setRevisionReady(elaClient *fakeclientset.Clientset, rev *v1alpha1.Revision) {
	// Wait a bit to kick off the ready event.
	time.Sleep(50 * time.Millisecond)
	rev.Status = v1alpha1.RevisionStatus{
		Conditions: []v1alpha1.RevisionCondition{
			{
				Type:   v1alpha1.RevisionConditionReady,
				Status: corev1.ConditionTrue,
			},
		},
	}
	elaClient.ElafrosV1alpha1().Revisions(rev.GetNamespace()).Update(rev)
}

func handleRequst(handler http.Handler, rr *httptest.ResponseRecorder, req *http.Request, signal chan bool) {
	handler.ServeHTTP(rr, req)
	signal <- true
}

func testHandlerRevision(t *testing.T, servingState v1alpha1.RevisionServingStateType, expectedStatus int, addRevToEnv bool) {
	rev := getTestRevision(servingState, testRevisionName)
	kubeClient := fakekubeclientset.NewSimpleClientset()
	elaClient := fakeclientset.NewSimpleClientset()
	if addRevToEnv {
		addTestRevisions(t, kubeClient, elaClient, rev)
	}
	signal := make(chan bool)
	a := getActivator(t, kubeClient, elaClient)
	go a.process()
	req := getHTTPRequest(t, testRevisionName)
	// response recorder to record the response
	responseRecorder := httptest.NewRecorder()
	// Our handlers satisfy http.Handler, so we can call their ServeHTTP method
	// directly and pass in our Request and ResponseRecorder.
	handler := http.HandlerFunc(a.handler)
	go handleRequst(handler, responseRecorder, req, signal)
	setRevisionReady(elaClient, rev)
	<-signal
	if status := responseRecorder.Code; status != expectedStatus {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, expectedStatus)
	}
}

// Test for a revision that's not in Elafros.
func TestHandler_revisionNotInElafros(t *testing.T) {
	testHandlerRevision(t, v1alpha1.RevisionServingStateActive, http.StatusNotFound, false)
}

// Test for a revision with reserve status.
func TestHandler_reserveRevision(t *testing.T) {
	testHandlerRevision(t, v1alpha1.RevisionServingStateReserve, http.StatusOK, true)
}

// Test for a revision with active status.
func TestHandler_activeRevision(t *testing.T) {
	testHandlerRevision(t, v1alpha1.RevisionServingStateActive, http.StatusOK, true)
}

// Test for a revision with reretired status.
func TestHandler_retiredRevision(t *testing.T) {
	testHandlerRevision(t, v1alpha1.RevisionServingStateRetired, http.StatusServiceUnavailable, true)
}

// Test for a revision with unknown status.
func TestHandler_unknowRevision(t *testing.T) {
	testHandlerRevision(t, "Unknown", http.StatusServiceUnavailable, true)
}

func testHandlerMultipleRevisions(t *testing.T, revMap map[*v1alpha1.Revision]int) {
	kubeClient := fakekubeclientset.NewSimpleClientset()
	elaClient := fakeclientset.NewSimpleClientset()
	count := 0
	for rev, num := range revMap {
		addTestRevisions(t, kubeClient, elaClient, rev)
		count += num
	}
	if count == 0 {
		return
	}

	signal := make(chan bool, count)
	responseRecorders := make([]*httptest.ResponseRecorder, count)
	a := getActivator(t, kubeClient, elaClient)
	go a.process()
	handler := http.HandlerFunc(a.handler)

	index := 0
	for rev, num := range revMap {
		for i := 0; i < num; i++ {
			req := getHTTPRequest(t, rev.GetName())
			responseRecorders[index] = httptest.NewRecorder()
			go handleRequst(handler, responseRecorders[index], req, signal)
			index++
		}
	}

	// Set the revisions to be ready
	for rev := range revMap {
		setRevisionReady(elaClient, rev)
	}

	// wait until all requests are done
	for i := 0; i < count; i++ {
		<-signal
	}

	for _, rr := range responseRecorders {
		if status := rr.Code; status != http.StatusOK {
			t.Errorf("handler returned wrong status code: got %v want %v",
				status, http.StatusOK)
		}
	}
}

// Test when there are 3 concurrent requests for a particular reserve revision.
func TestHandler_threeConcurrentReserveRevisions(t *testing.T) {
	rev := getTestRevision(v1alpha1.RevisionServingStateReserve, testRevisionName)
	revMap := make(map[*v1alpha1.Revision]int)
	revMap[rev] = 3
	testHandlerMultipleRevisions(t, revMap)
}

// Test when there are 2 concurrent requests for a particular active revision.
func TestHandler_twoConcurrentActiveRevisions(t *testing.T) {
	rev := getTestRevision(v1alpha1.RevisionServingStateActive, testRevisionName)
	revMap := make(map[*v1alpha1.Revision]int)
	revMap[rev] = 2
	testHandlerMultipleRevisions(t, revMap)
}

// Test when there are two unique revisions, revisionMap has more than one entries.
func TestHandler_twoUniqueRevisions(t *testing.T) {
	rev1 := getTestRevision(v1alpha1.RevisionServingStateActive, "test-rev1")
	rev2 := getTestRevision(v1alpha1.RevisionServingStateReserve, "test-rev2")
	revMap := make(map[*v1alpha1.Revision]int)
	revMap[rev1] = 3
	revMap[rev2] = 2
	testHandlerMultipleRevisions(t, revMap)
}
