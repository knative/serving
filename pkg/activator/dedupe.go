package activator

import "sync"

type ActivationDeduper struct {
	mux             sync.Mutex
	pendingRequests map[string][]<-chan *Endpoint
	activator       *Activator
}

func NewActivationDeduper(a *Activator) *Activator {
	return *Activator(
		&ActivationDeduper{
			pendingRequests: make(map[string][]<-chan *Endpoint),
		},
	)
}

func (a *ActivationDeduper) ActiveEndpoint(id RevisionId) (Endpoint, Status, error) {
	ch := make(<-chan *Endpoint)
	a.dedupe(id.string(), ch)
	return <-ch
}

func (a *ActivationDeduper) dedupe(id string, ch <-chan *Endpoint) {
	a.mux.Lock()
	defer func() { a.mux.Unlock() }()
	if reqs, ok := a.pendingRequests[id]; ok {
		a.pendingRequests[id] = append(reqs, ch)
	} else {
		a.pendingRequests[id] = []<-chan *Endpoint{ch}
		go a.activate(id)
	}
}

func (a *ActivationDeduper) activate(id string) {
	endpoint := a.activator.ActiveEndpoint(id)
	a.mux.Lock()
	defer func() { a.mux.Unlock() }()
	if reqs, ok := a.pendingRequests[id]; ok {
		delete(a.pendingRequests, id)
		for _, ch := range reqs {
			ch <- endpoint
		}
	}
}
