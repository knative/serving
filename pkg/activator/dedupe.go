package activator

import "sync"

type activationResult struct {
	endpoint Endpoint
	status   Status
	err      error
}

type DeduppingActivator struct {
	mux             sync.Mutex
	pendingRequests map[revisionId][]chan activationResult
	activator       Activator
}

func NewDeduppingActivator(a Activator) Activator {
	return Activator(
		&DeduppingActivator{
			pendingRequests: make(map[revisionId][]chan activationResult),
			activator:       a,
		},
	)
}

func (a *DeduppingActivator) ActiveEndpoint(namespace, name string) (Endpoint, Status, error) {
	id := revisionId{namespace: namespace, name: name}
	ch := make(chan activationResult, 1)
	a.dedupe(id, ch)
	result := <-ch
	return result.endpoint, result.status, result.err
}

func (a *DeduppingActivator) dedupe(id revisionId, ch chan activationResult) {
	a.mux.Lock()
	defer func() { a.mux.Unlock() }()
	if reqs, ok := a.pendingRequests[id]; ok {
		a.pendingRequests[id] = append(reqs, ch)
	} else {
		a.pendingRequests[id] = []chan activationResult{ch}
		go a.activate(id)
	}
}

func (a *DeduppingActivator) activate(id revisionId) {
	endpoint, status, err := a.activator.ActiveEndpoint(id.namespace, id.name)
	a.mux.Lock()
	defer func() { a.mux.Unlock() }()
	result := activationResult{
		endpoint: endpoint,
		status:   status,
		err:      err,
	}
	if reqs, ok := a.pendingRequests[id]; ok {
		delete(a.pendingRequests, id)
		for _, ch := range reqs {
			ch <- result
		}
	}
}
