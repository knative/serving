package activator

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestSingleRevision_SingleRequest_Success(t *testing.T) {
	want := Endpoint{"ip", 8080}
	f := newFakeActivator(t,
		map[revisionId]activationResult{
			revisionId{"default", "rev1"}: activationResult{
				endpoint: want,
				status:   Status(0),
				err:      nil,
			},
		})
	d := NewActivationDeduper(Activator(f))

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
		map[revisionId]activationResult{
			revisionId{"default", "rev1"}: activationResult{
				endpoint: ep,
				status:   Status(0),
				err:      nil,
			},
		})
	d := NewActivationDeduper(f)

	got := concurrentTest(d, f, []revisionId{
		revisionId{"default", "rev1"},
		revisionId{"default", "rev1"},
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
		map[revisionId]activationResult{
			revisionId{"default", "rev1"}: activationResult{
				endpoint: ep1,
				status:   Status(0),
				err:      nil,
			},
			revisionId{"default", "rev2"}: activationResult{
				endpoint: ep2,
				status:   Status(0),
				err:      nil,
			},
		})
	d := NewActivationDeduper(f)

	got := concurrentTest(d, f, []revisionId{
		revisionId{"default", "rev1"},
		revisionId{"default", "rev2"},
		revisionId{"default", "rev1"},
		revisionId{"default", "rev2"},
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
	status2 := Status(500)
	error2 := fmt.Errorf("Test error")
	f := newFakeActivator(t,
		map[revisionId]activationResult{
			revisionId{"default", "rev1"}: activationResult{
				endpoint: ep1,
				status:   Status(0),
				err:      nil,
			},
			revisionId{"default", "rev2"}: activationResult{
				endpoint: Endpoint{},
				status:   status2,
				err:      error2,
			},
		})
	d := NewActivationDeduper(f)

	got := concurrentTest(d, f, []revisionId{
		revisionId{"default", "rev1"},
		revisionId{"default", "rev2"},
		revisionId{"default", "rev1"},
		revisionId{"default", "rev2"},
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
	failErr := fmt.Errorf("Test error.")
	successEp := Endpoint{"ip", 8080}
	successStatus := Status(0)
	f := newFakeActivator(t,
		map[revisionId]activationResult{
			revisionId{"default", "rev1"}: activationResult{
				endpoint: failEp,
				status:   failStatus,
				err:      failErr,
			},
		})
	d := NewActivationDeduper(Activator(f))

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
	f.responses[revisionId{"default", "rev1"}] = activationResult{
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

type fakeActivator struct {
	t         *testing.T
	responses map[revisionId]activationResult
	record    []revisionId
	holds     map[revisionId]chan struct{}
}

func newFakeActivator(t *testing.T, responses map[revisionId]activationResult) *fakeActivator {
	return &fakeActivator{
		t:         t,
		responses: responses,
		record:    make([]revisionId, 0),
		holds:     make(map[revisionId]chan struct{}),
	}
}

func (f *fakeActivator) ActiveEndpoint(namespace, name string) (Endpoint, Status, error) {
	id := revisionId{namespace, name}
	f.record = append(f.record, id)
	if result, ok := f.responses[id]; ok {
		if hold, ok := f.holds[id]; ok {
			<-hold
		}
		return result.endpoint, result.status, result.err
	} else {
		f.t.Fatalf("Unexpected call to activator: %v", id)
		return Endpoint{}, Status(0), nil
	}
}

func (f *fakeActivator) hold(id revisionId) {
	if _, ok := f.holds[id]; !ok {
		f.holds[id] = make(chan struct{})
	}
}

func (f *fakeActivator) release(id revisionId) {
	if h, ok := f.holds[id]; ok {
		close(h)
		delete(f.holds, id)
	}
}

func concurrentTest(a Activator, f *fakeActivator, ids []revisionId) []activationResult {
	for _, id := range ids {
		f.hold(id)
	}
	var start sync.WaitGroup
	var end sync.WaitGroup
	results := make([]activationResult, len(ids))
	for i, id := range ids {
		start.Add(1)
		end.Add(1)
		go func(index int, id revisionId) {
			start.Done()
			endpoint, status, err := a.ActiveEndpoint(id.namespace, id.name)
			results[index] = activationResult{endpoint, status, err}
			end.Done()
		}(i, id)
	}
	start.Wait()
	for _, id := range ids {
		f.release(id)
	}
	end.Wait()
	return results
}
