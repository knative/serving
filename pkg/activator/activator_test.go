package activator

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestActivatorOneRequest(t *testing.T) {
	done := make(chan struct{})
	ch := newTestChannels().
		withRevisionActivator(succeedingRevisionActivator(done, "ip", 8080)).
		withProxy(succeedingProxy())
	a := ch.newActivator()

	ch.sendHttpRequest("rev1")
	close(done)

	// assert one activation request was made
	// assert one request was proxied
}

type testChannels struct {
	done               chan *HttpRequest
	httpRequests       chan *HttpRequest
	activationRequests chan *RevisionId
	endpoints          chan *RevisionEndpoint
	proxyRequests      chan *ProxyRequest
}

func newTestChannels() *testChannels {
	return &testChannels{
		httpRequests:       make(chan *HttpRequest),
		activationRequests: make(chan *RevisionId),
		endpoints:          make(chan *RevisionEndpoint),
		proxyRequests:      make(chan *ProxyRequest),
	}
}

func (ch *testChannels) newActivator() *Activator {
	return NewActivator(
		ch.httpRequests.(<-chan *HttpRequest),
		ch.activationRequests.(chan<- *RevisionId),
		ch.endpoints.(<-chan *RevisionEndpoint),
		ch.proxyRequests.(chan<- *ProxyRequest))
}

func (ch *testChannels) sendHttpRequest(name string) {
	ch.httpRequests <- &HttpRequest{
		w: httptest.NewRecorder(),
		r: &http.Request{
			Header: Header(map[string][]string{
				"Elafros-Revision": []string{name},
			}),
		},
	}
}

type fakeRevisionActivator func(*RevisionId) *RevisionEndpoint
type fakeProxy func(*ProxyRequest)

func (ch *testChannels) withRevisionActivator(f fakeRevisionActivator) *testChannels {
	go func() {
		for {
			rev := <-ch.activationRequests
			endpoints <- f(rev)
		}
	}()
}

func (ch *testChannels) withProxy(f fakeProxy) *testChannels {
	go func() {
		for {
			f(<-ch.proxyRequests)
		}
	}()
}

func succeedingRevisionActivator(release chan struct{}, ip string, int32 port) fakeRevisionActivator {
	return fakeRevisionActivator(
		func(r *RevisionId) *RevisionEndpoint {
			_ <- release
			return &RevisionEndpoint{
				RevisionId: *r,
				endpoint: endpoint{
					ip:   ip,
					port: port,
				},
			}
		},
	)
}

func failingRevisionActivator(err error, status int) fakeRevisionActivator {
	return fakeRevisionActivator(
		func(r *RevisionId) *RevisionEndpoint {
			return &RevisionEndpoint{
				RevisionId: *r,
				err:        err,
				status:     status,
			}
		},
	)
}

func succeedingProxy() fakeProxy {
	return fakeProxy(
		func(_ *ProxyRequest) {},
	)
}
