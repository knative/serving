/*
Copyright 2018 Google LLC

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
		map[revisionID]activationResult{
			revisionID{"default", "rev1"}: activationResult{
				endpoint: want,
				status:   Status(0),
				err:      nil,
			},
		})
	d := NewDedupingActivator(Activator(f))

	endpoint, status, err := d.ActiveEndpoint("default", "rev1")

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if endpoint != want {
		t.Errorf("Unexpected endpoint. Want %+v. Got %+v.", want, endpoint)
	}
	if status != 0 {
		t.Errorf("Unexpected status. Want 0. Got %v.", status)
	}
	if len(f.record) != 1 {
		t.Errorf("Unexpected number of activation requests. Want 1. Got %v.", len(f.record))
	}
}

func TestSingleRevision_MultipleRequests_Success(t *testing.T) {
	ep := Endpoint{"ip", 8080}
	f := newFakeActivator(t,
		map[revisionID]activationResult{
			revisionID{"default", "rev1"}: activationResult{
				endpoint: ep,
				status:   Status(0),
				err:      nil,
			},
		})
	d := NewDedupingActivator(f)

	got := concurrentTest(d, f, []revisionID{
		revisionID{"default", "rev1"},
		revisionID{"default", "rev1"},
	})

	want := []activationResult{
		activationResult{ep, Status(0), nil},
		activationResult{ep, Status(0), nil},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Unexpected results. Wanted %+v. Got %+v.", want, got)
	}
	if len(f.record) != 1 {
		t.Errorf("Unexpected number of activation requests. Want 1. Got %v.", len(f.record))
	}
}

func TestMultipleRevisions_MultipleRequests_Success(t *testing.T) {
	ep1 := Endpoint{"ip1", 8080}
	ep2 := Endpoint{"ip2", 8080}
	f := newFakeActivator(t,
		map[revisionID]activationResult{
			revisionID{"default", "rev1"}: activationResult{
				endpoint: ep1,
				status:   Status(0),
				err:      nil,
			},
			revisionID{"default", "rev2"}: activationResult{
				endpoint: ep2,
				status:   Status(0),
				err:      nil,
			},
		})
	d := NewDedupingActivator(f)

	got := concurrentTest(d, f, []revisionID{
		revisionID{"default", "rev1"},
		revisionID{"default", "rev2"},
		revisionID{"default", "rev1"},
		revisionID{"default", "rev2"},
	})

	want := []activationResult{
		activationResult{ep1, Status(0), nil},
		activationResult{ep2, Status(0), nil},
		activationResult{ep1, Status(0), nil},
		activationResult{ep2, Status(0), nil},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Unexpected results. \nWant %+v. \nGot %+v", want, got)
	}
	if len(f.record) != 2 {
		t.Errorf("Unexpected number of activation requests. Want 2. Got %v.", len(f.record))
	}
}

func TestMultipleRevisions_MultipleRequests_PartialSuccess(t *testing.T) {
	ep1 := Endpoint{"ip1", 8080}
	status2 := Status(http.StatusInternalServerError)
	error2 := fmt.Errorf("test error")
	f := newFakeActivator(t,
		map[revisionID]activationResult{
			revisionID{"default", "rev1"}: activationResult{
				endpoint: ep1,
				status:   Status(0),
				err:      nil,
			},
			revisionID{"default", "rev2"}: activationResult{
				endpoint: Endpoint{},
				status:   status2,
				err:      error2,
			},
		})
	d := NewDedupingActivator(f)

	got := concurrentTest(d, f, []revisionID{
		revisionID{"default", "rev1"},
		revisionID{"default", "rev2"},
		revisionID{"default", "rev1"},
		revisionID{"default", "rev2"},
	})

	want := []activationResult{
		activationResult{ep1, Status(0), nil},
		activationResult{Endpoint{}, status2, error2},
		activationResult{ep1, Status(0), nil},
		activationResult{Endpoint{}, status2, error2},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Unexpected results. \nWant %+v. \nGot %+v", want, got)
	}
	if len(f.record) != 2 {
		t.Errorf("Unexpected number of activation requests. Want 2. Got %v.", len(f.record))
	}
}

func TestSingleRevision_MultipleRequests_FailureRecovery(t *testing.T) {
	failEp := Endpoint{}
	failStatus := Status(503)
	failErr := fmt.Errorf("test error")
	f := newFakeActivator(t,
		map[revisionID]activationResult{
			revisionID{"default", "rev1"}: activationResult{
				endpoint: failEp,
				status:   failStatus,
				err:      failErr,
			},
		})
	d := NewDedupingActivator(Activator(f))

	// Activation initially fails
	endpoint, status, err := d.ActiveEndpoint("default", "rev1")

	if err != failErr {
		t.Errorf("Unexpected error. Want %v. Got %v.", failErr, err)
	}
	if endpoint != failEp {
		t.Errorf("Unexpected endpoint. Want %+v. Got %+v.", failEp, endpoint)
	}
	if status != failStatus {
		t.Errorf("Unexpected status. Want %v. Got %v.", failStatus, status)
	}
	if len(f.record) != 1 {
		t.Errorf("Unexpected number of activation requests. Want 1. Got %v.", len(f.record))
	}

	// Later activation succeeds
	successEp := Endpoint{"ip", 8080}
	successStatus := Status(0)
	f.responses[revisionID{"default", "rev1"}] = activationResult{
		endpoint: successEp,
		status:   successStatus,
		err:      nil,
	}

	endpoint, status, err = d.ActiveEndpoint("default", "rev1")

	if err != nil {
		t.Errorf("Unexpected error. Want %v. Got %v.", nil, err)
	}
	if endpoint != successEp {
		t.Errorf("Unexpected endpoint. Want %+v. Got %+v.", successEp, endpoint)
	}
	if status != successStatus {
		t.Errorf("Unexpected status. Want %v. Got %v.", successStatus, status)
	}
	if len(f.record) != 2 {
		t.Errorf("Unexpected number of activation requests. Want 2. Got %v.", len(f.record))
	}
}

func TestShutdown_ReturnError(t *testing.T) {
	ep := Endpoint{"ip", 8080}
	f := newFakeActivator(t,
		map[revisionID]activationResult{
			revisionID{"default", "rev1"}: activationResult{
				endpoint: ep,
				status:   Status(0),
				err:      nil,
			},
		})
	d := NewDedupingActivator(Activator(f))
	f.hold(revisionID{"default", "rev1"})

	go func() {
		time.Sleep(100 * time.Millisecond)
		d.Shutdown()
	}()
	endpoint, status, err := d.ActiveEndpoint("default", "rev1")

	want := Endpoint{}
	if endpoint != want {
		t.Errorf("Unexpected endpoint. Want %+v. Got %+v.", want, endpoint)
	}
	if status != Status(http.StatusInternalServerError) {
		t.Errorf("Unexpected error stats. Want %v. Got %v.", http.StatusInternalServerError, status)
	}
	if err == nil {
		t.Errorf("Expected error. Want error. Got nil.")
	}
}

type fakeActivator struct {
	t         *testing.T
	responses map[revisionID]activationResult
	record    []revisionID
	holds     map[revisionID]chan struct{}
}

func newFakeActivator(t *testing.T, responses map[revisionID]activationResult) *fakeActivator {
	return &fakeActivator{
		t:         t,
		responses: responses,
		record:    make([]revisionID, 0),
		holds:     make(map[revisionID]chan struct{}),
	}
}

func (f *fakeActivator) ActiveEndpoint(namespace, name string) (Endpoint, Status, error) {
	id := revisionID{namespace, name}
	f.record = append(f.record, id)
	if result, ok := f.responses[id]; ok {
		if hold, ok := f.holds[id]; ok {
			<-hold
		}
		return result.endpoint, result.status, result.err
	}

	f.t.Fatalf("Unexpected call to activator: %v", id)
	return Endpoint{}, Status(0), nil
}

func (f *fakeActivator) Shutdown() {
	// Nothing to do.
}

func (f *fakeActivator) hold(id revisionID) {
	if _, ok := f.holds[id]; !ok {
		f.holds[id] = make(chan struct{})
	}
}

func (f *fakeActivator) release(id revisionID) {
	if h, ok := f.holds[id]; ok {
		close(h)
		delete(f.holds, id)
	}
}

func concurrentTest(a Activator, f *fakeActivator, ids []revisionID) []activationResult {
	for _, id := range ids {
		f.hold(id)
	}
	var start sync.WaitGroup
	var end sync.WaitGroup
	results := make([]activationResult, len(ids))
	for i, id := range ids {
		start.Add(1)
		end.Add(1)
		go func(index int, id revisionID) {
			start.Done()
			endpoint, status, err := a.ActiveEndpoint(id.namespace, id.name)
			results[index] = activationResult{endpoint, status, err}
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
