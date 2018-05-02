package activator

import (
	"testing"
)

func TestSingleRevisionSingleRequest(t *testing.T) {
	id := &RevisionId{
		Namespace: "default",
		Name:      "rev1",
	}
	f := newFakeActivator(t,
		map[RevisionId]*RevisionEndpoint{
			"default/rev": &RevisionEndpoint{
				Ip:   "ip",
				Port: 8080,
			},
		})
	d := NewActivationDeduper(*Activator(f))

	endpoint, status, err := d.ActiveEndpoint(id)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(a.record) != 1 {
		t.Errorf("Unexpected number of activation requests. Want 1. Got %v.", len(a.record))
	}
}

type fakeActivator struct {
	t         *testing.T
	responses map[RevisionId]*RevisionEndpoint
	record    []*RevisionId
}

func newFakeActivator(t *testing.T, responses map[string]*RevisionEndpoint) *fakeActivator {
	return &fakeActivator{
		t:         t,
		responses: responses,
		record:    make([]*RevisionId, 0),
	}
}

func (f *fakeActivator) ActiveEndpoint(id *RevisionId) *RevisionEndpoint {
	f.record = append(f.record, id)
	if endpoint, ok := f.responses[id.string()]; ok {
		return endpoint
	} else {
		f.t.Errorf("Unexpected call to activator: %v", id)
	}
}
