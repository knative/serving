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
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestSingleRevision_SingleRequest_Success(t *testing.T) {
	want := Endpoint{"ip", 8080}
	f := newFakeActivator(t,
		map[revisionID]ActivationResult{
			revisionID{testNamespace, testRevision}: {
				Endpoint: want,
				Status:   http.StatusOK,
			},
		})
	d := NewDedupingActivator(Activator(f))

	ar := d.ActiveEndpoint(testNamespace, testRevision)

	if ar.Error != nil {
		t.Errorf("Unexpected error: %v", ar.Error)
	}
	if ar.Endpoint != want {
		t.Errorf("Unexpected endpoint. Want %+v. Got %+v.", want, ar.Endpoint)
	}
	if ar.Status != http.StatusOK {
		t.Errorf("Unexpected status. Want http.StatusOK. Got %v.", ar.Status)
	}
	if len(f.record) != 1 {
		t.Errorf("Unexpected number of activation requests. Want 1. Got %v.", len(f.record))
	}
}

func TestSingleRevision_MultipleRequests_Success(t *testing.T) {
	ep := Endpoint{"ip", 8080}
	f := newFakeActivator(t,
		map[revisionID]ActivationResult{
			revisionID{testNamespace, testRevision}: {
				Endpoint: ep,
				Status:   http.StatusOK,
			},
		})
	d := NewDedupingActivator(f)

	got := concurrentTest(d, f, []revisionID{
		{testNamespace, testRevision},
		{testNamespace, testRevision},
	})

	want := []ActivationResult{
		{http.StatusOK, ep, "", "", nil},
		{http.StatusOK, ep, "", "", nil},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Unexpected results. Wanted %+v. Got %+v.", want, got)
	}
	if len(f.record) != 1 {
		t.Errorf("Unexpected number of activation requests. Want 1. Got %v.", len(f.record))
	}
}

func TestMultipleRevisions_MultipleRequests_Success(t *testing.T) {
	_, kna := fakeClients()
	kna.ServingV1alpha1().Revisions(testNamespace).Create(
		newRevisionBuilder(defaultRevisionLabels).withRevisionName("rev1").build())
	kna.ServingV1alpha1().Revisions(testNamespace).Create(
		newRevisionBuilder(defaultRevisionLabels).withRevisionName("rev2").build())
	ep1 := Endpoint{"ip1", 8080}
	ep2 := Endpoint{"ip2", 8080}
	f := newFakeActivator(t,
		map[revisionID]ActivationResult{
			revisionID{testNamespace, "rev1"}: {
				Endpoint: ep1,
				Status:   http.StatusOK,
			},
			revisionID{testNamespace, "rev2"}: {
				Endpoint: ep2,
				Status:   http.StatusOK,
			},
		})
	d := NewDedupingActivator(f)

	got := concurrentTest(d, f, []revisionID{
		{testNamespace, "rev1"},
		{testNamespace, "rev2"},
		{testNamespace, "rev1"},
		{testNamespace, "rev2"},
	})

	want := []ActivationResult{
		{http.StatusOK, ep1, "", "", nil},
		{http.StatusOK, ep2, "", "", nil},
		{http.StatusOK, ep1, "", "", nil},
		{http.StatusOK, ep2, "", "", nil},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Unexpected results. \nWant %+v. \nGot %+v", want, got)
	}
	if len(f.record) != 2 {
		t.Errorf("Unexpected number of activation requests. Want 2. Got %v. %v", len(f.record), f.record)
	}
}

func TestMultipleRevisions_MultipleRequests_PartialSuccess(t *testing.T) {
	_, kna := fakeClients()
	kna.ServingV1alpha1().Revisions(testNamespace).Create(
		newRevisionBuilder(defaultRevisionLabels).withRevisionName("rev1").build())
	kna.ServingV1alpha1().Revisions(testNamespace).Create(
		newRevisionBuilder(defaultRevisionLabels).withRevisionName("rev2").build())
	ep1 := Endpoint{"ip1", 8080}
	status2 := http.StatusInternalServerError
	error2 := fmt.Errorf("test error")
	f := newFakeActivator(t,
		map[revisionID]ActivationResult{
			revisionID{testNamespace, "rev1"}: {
				Endpoint: ep1,
				Status:   http.StatusOK,
			},
			revisionID{testNamespace, "rev2"}: {
				Endpoint: Endpoint{},
				Status:   status2,
				Error:    error2,
			},
		})
	d := NewDedupingActivator(f)

	got := concurrentTest(d, f, []revisionID{
		{testNamespace, "rev1"},
		{testNamespace, "rev2"},
		{testNamespace, "rev1"},
		{testNamespace, "rev2"},
	})

	want := []ActivationResult{
		{http.StatusOK, ep1, "", "", nil},
		{status2, Endpoint{}, "", "", error2},
		{http.StatusOK, ep1, "", "", nil},
		{status2, Endpoint{}, "", "", error2},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Unexpected results. \nWant %+v. \nGot %+v", want, got)
	}
	if len(f.record) != 2 {
		t.Errorf("Unexpected number of activation requests. Want 2. Got %v.", len(f.record))
	}
}

func TestSingleRevision_MultipleRequests_FailureRecovery(t *testing.T) {
	_, kna := fakeClients()
	kna.ServingV1alpha1().Revisions(testNamespace).Create(
		newRevisionBuilder(defaultRevisionLabels).build())
	failEp := Endpoint{}
	failStatus := http.StatusServiceUnavailable
	failErr := fmt.Errorf("test error")
	f := newFakeActivator(t,
		map[revisionID]ActivationResult{
			revisionID{testNamespace, testRevision}: {
				Endpoint: failEp,
				Status:   failStatus,
				Error:    failErr,
			},
		})
	d := NewDedupingActivator(Activator(f))

	// Activation initially fails
	ar := d.ActiveEndpoint(testNamespace, testRevision)

	if ar.Error != failErr {
		t.Errorf("Unexpected error. Want %v. Got %v.", failErr, ar.Error)
	}
	if ar.Endpoint != failEp {
		t.Errorf("Unexpected endpoint. Want %+v. Got %+v.", failEp, ar.Endpoint)
	}
	if ar.Status != failStatus {
		t.Errorf("Unexpected status. Want %v. Got %v.", failStatus, ar.Status)
	}
	if len(f.record) != 1 {
		t.Errorf("Unexpected number of activation requests. Want 1. Got %v.", len(f.record))
	}

	// Later activation succeeds
	successEp := Endpoint{"ip", 8080}
	successStatus := http.StatusOK
	f.responses[revisionID{testNamespace, testRevision}] = ActivationResult{
		Endpoint: successEp,
		Status:   successStatus,
	}

	ar = d.ActiveEndpoint(testNamespace, testRevision)

	if ar.Error != nil {
		t.Errorf("Unexpected error. Want %v. Got %v.", nil, ar.Error)
	}
	if ar.Endpoint != successEp {
		t.Errorf("Unexpected endpoint. Want %+v. Got %+v.", successEp, ar.Endpoint)
	}
	if ar.Status != successStatus {
		t.Errorf("Unexpected status. Want %v. Got %v.", successStatus, ar.Status)
	}
	if len(f.record) != 2 {
		t.Errorf("Unexpected number of activation requests. Want 2. Got %v.", len(f.record))
	}
}

func TestShutdown_ReturnError(t *testing.T) {
	_, kna := fakeClients()
	kna.ServingV1alpha1().Revisions(testNamespace).Create(
		newRevisionBuilder(defaultRevisionLabels).build())
	ep := Endpoint{"ip", 8080}
	f := newFakeActivator(t,
		map[revisionID]ActivationResult{
			revisionID{testNamespace, testRevision}: {
				Endpoint: ep,
				Status:   http.StatusOK,
			},
		})
	d := NewDedupingActivator(Activator(f))
	f.hold(revisionID{testNamespace, testRevision})

	go func() {
		time.Sleep(100 * time.Millisecond)
		d.Shutdown()
	}()
	ar := d.ActiveEndpoint(testNamespace, testRevision)

	want := Endpoint{}
	if ar.Endpoint != want {
		t.Errorf("Unexpected endpoint. Want %+v. Got %+v.", want, ar.Endpoint)
	}
	if ar.Status != http.StatusInternalServerError {
		t.Errorf("Unexpected error stats. Want %v. Got %v.", http.StatusInternalServerError, ar.Status)
	}
	if ar.Error == nil {
		t.Errorf("Expected error. Want error. Got nil.")
	}
}

type fakeActivator struct {
	t         *testing.T
	responses map[revisionID]ActivationResult
	holds     map[revisionID]*sync.WaitGroup

	record      []revisionID
	recordMutex sync.Mutex
}

func newFakeActivator(t *testing.T, responses map[revisionID]ActivationResult) *fakeActivator {
	return &fakeActivator{
		t:         t,
		responses: responses,
		holds:     make(map[revisionID]*sync.WaitGroup),
		record:    make([]revisionID, 0),
	}
}

func (f *fakeActivator) ActiveEndpoint(namespace, name string) ActivationResult {
	id := revisionID{namespace, name}

	f.recordMutex.Lock()
	f.record = append(f.record, id)
	f.recordMutex.Unlock()

	if result, ok := f.responses[id]; ok {
		if hold, ok := f.holds[id]; ok {
			hold.Wait()
		}
		return result
	}

	f.t.Fatalf("Unexpected call to activator: %v", id)
	return ActivationResult{}
}

func (f *fakeActivator) Shutdown() {
	// Nothing to do.
}

func (f *fakeActivator) hold(id revisionID) {
	_, ok := f.holds[id]

	if !ok {
		f.holds[id] = new(sync.WaitGroup)
	}

	f.holds[id].Add(1)
}

func (f *fakeActivator) release(id revisionID) {
	if h, ok := f.holds[id]; ok {
		h.Done()
	}
}

func concurrentTest(a Activator, f *fakeActivator, ids []revisionID) []ActivationResult {
	for _, id := range ids {
		f.hold(id)
	}
	var start sync.WaitGroup
	var end sync.WaitGroup
	results := make([]ActivationResult, len(ids))
	for i, id := range ids {
		start.Add(1)
		end.Add(1)
		go func(index int, id revisionID) {
			start.Done()
			results[index] = a.ActiveEndpoint(id.namespace, id.name)
			end.Done()
		}(i, id)
	}
	start.Wait()
	time.Sleep(100 * time.Millisecond) // wait for concurrent requests to land
	for _, id := range ids {
		f.release(id)
	}
	end.Wait()
	return results
}
