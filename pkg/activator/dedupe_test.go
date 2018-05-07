package activator

import (
	"reflect"
	"sync"
	"testing"
)

func TestSingleRevisionSingleRequest(t *testing.T) {
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

func TestSingleRevisionMultipleRequests(t *testing.T) {
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
		go func(index int) {
			start.Done()
			endpoint, status, err := a.ActiveEndpoint(id.namespace, id.name)
			results[index] = activationResult{endpoint, status, err}
			end.Done()
		}(i)
	}
	start.Wait()
	for _, id := range ids {
		f.release(id)
	}
	end.Wait()
	return results
}
