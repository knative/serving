package activator

import (
	"fmt"
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

type fakeActivator struct {
	t         *testing.T
	responses map[revisionId]activationResult
	record    []revisionId
}

func newFakeActivator(t *testing.T, responses map[revisionId]activationResult) *fakeActivator {
	return &fakeActivator{
		t:         t,
		responses: responses,
		record:    make([]revisionId, 0),
	}
}

func (f *fakeActivator) ActiveEndpoint(namespace, name string) (Endpoint, Status, error) {
	id := revisionId{namespace, name}
	f.record = append(f.record, id)
	fmt.Printf("id: %+v", id)
	fmt.Printf("responses: %+v", f.responses)
	if result, ok := f.responses[id]; ok {
		return result.endpoint, result.status, result.err
	} else {
		f.t.Fatalf("Unexpected call to activator: %v", id)
		return Endpoint{}, Status(0), nil
	}
}
