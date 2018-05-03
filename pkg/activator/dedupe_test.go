package activator

import (
	"fmt"
	"testing"
)

func TestSingleRevisionSingleRequest(t *testing.T) {
	id := RevisionId{
		namespace: "default",
		name:      "rev1",
	}
	f := newFakeActivator(t,
		map[RevisionId]activationResult{
			RevisionId{namespace: "default", name: "rev1"}: activationResult{
				endpoint: Endpoint{
					Ip:   "ip",
					Port: 8080,
				},
				status: Status(0),
				err:    nil,
			},
		})
	d := NewActivationDeduper(Activator(f))

	endpoint, status, err := d.ActiveEndpoint(id)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	wantEndpoint := Endpoint{"ip", 8080}
	if endpoint != wantEndpoint {
		t.Errorf("Unexpected endpoint. Want %+v. Got %+v.", wantEndpoint, endpoint)
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
	responses map[RevisionId]activationResult
	record    []RevisionId
}

func newFakeActivator(t *testing.T, responses map[RevisionId]activationResult) *fakeActivator {
	return &fakeActivator{
		t:         t,
		responses: responses,
		record:    make([]RevisionId, 0),
	}
}

func (f *fakeActivator) ActiveEndpoint(id RevisionId) (Endpoint, Status, error) {
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
