package activator

import "sync"

type activationResult struct {
	endpoint Endpoint
	status   Status
	err      error
}

type ActivationDeduper struct {
	mux             sync.Mutex
	pendingRequests map[string][]chan activationResult
	activator       Activator
}

func NewActivationDeduper(a Activator) Activator {
	return Activator(
		&ActivationDeduper{
			pendingRequests: make(map[string][]chan activationResult),
			activator:       a,
		},
	)
}

func (a *ActivationDeduper) ActiveEndpoint(id RevisionId) (Endpoint, Status, error) {
	ch := make(chan activationResult, 1)
	a.dedupe(id, ch)
	result := <-ch
	return result.endpoint, result.status, result.err
}

func (a *ActivationDeduper) dedupe(id RevisionId, ch chan activationResult) {
	a.mux.Lock()
	defer func() { a.mux.Unlock() }()
	if reqs, ok := a.pendingRequests[id.string()]; ok {
		a.pendingRequests[id.string()] = append(reqs, ch)
	} else {
		a.pendingRequests[id.string()] = []chan activationResult{ch}
		go a.activate(id)
	}
}

func (a *ActivationDeduper) activate(id RevisionId) {
	endpoint, status, err := a.activator.ActiveEndpoint(id)
	a.mux.Lock()
	defer func() { a.mux.Unlock() }()
	result := activationResult{
		endpoint: endpoint,
		status:   status,
		err:      err,
	}
	if reqs, ok := a.pendingRequests[id.string()]; ok {
		delete(a.pendingRequests, id.string())
		for _, ch := range reqs {
			ch <- result
		}
	}
}
