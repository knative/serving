package activator

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func TestActivatorOneRequest(t *testing.T) {
	done := make(chan struct{})
	at := newActivatorTest().
		withActivator().
		withFakeRevisionActivatorFn(succeedingRevisionActivatorFn(done, "ip", 8080)).
		withFakeProxyFn(succeedingProxyFn())

	at.sendHttpRequest("rev1")
	close(done)
	at.done()

	if len(at.fakeRevisionActivator.record) != 1 {
		t.Fatalf("Unexpected number of revision activations. Want 1. Got %v.",
			len(at.fakeRevisionActivator.record))
	}
	if len(at.fakeProxy.record) != 1 {
		t.Fatalf("Unexpected number of requests proxied. Want 1. Got %v.",
			len(at.fakeProxy.record))
	}
}

type activatorTest struct {
	httpRequests          chan *HttpRequest
	activationRequests    chan *RevisionId
	endpoints             chan *RevisionEndpoint
	proxyRequests         chan *ProxyRequest
	activator             *Activator
	revisionActivator     *RevisionActivator
	proxy                 *Proxy
	fakeRevisionActivator *fakeRevisionActivator
	fakeProxy             *fakeProxy
}

func newActivatorTest() *activatorTest {
	return &activatorTest{
		httpRequests:       make(chan *HttpRequest),
		activationRequests: make(chan *RevisionId),
		endpoints:          make(chan *RevisionEndpoint),
		proxyRequests:      make(chan *ProxyRequest),
	}
}

func (at *activatorTest) withActivator() *activatorTest {
	at.activator = NewActivator(
		(<-chan *HttpRequest)(at.httpRequests),
		(chan<- *RevisionId)(at.activationRequests),
		(<-chan *RevisionEndpoint)(at.endpoints),
		(chan<- *ProxyRequest)(at.proxyRequests))
	return at
}

func (at *activatorTest) sendHttpRequest(name string) {
	at.httpRequests <- &HttpRequest{
		w: httptest.NewRecorder(),
		r: &http.Request{
			Header: http.Header(map[string][]string{
				"Elafros-Revision": []string{name},
			}),
		},
	}
}

func (at *activatorTest) done() {
	for i := 0; i < 10; i++ {
		runtime.Gosched()
	}
}

type fakeRevisionActivator struct {
	fn     func(*RevisionId) *RevisionEndpoint
	record map[*RevisionId]*RevisionEndpoint
}

func (at *activatorTest) withFakeRevisionActivatorFn(fn func(*RevisionId) *RevisionEndpoint) *activatorTest {
	record := make(map[*RevisionId]*RevisionEndpoint)
	at.fakeRevisionActivator = &fakeRevisionActivator{
		fn:     fn,
		record: record,
	}
	go func() {
		for {
			rev := <-at.activationRequests
			end := fn(rev)
			record[rev] = end
			at.endpoints <- end
		}
	}()
	return at
}

type fakeProxy struct {
	fn     func(*ProxyRequest)
	record []*ProxyRequest
}

func (at *activatorTest) withFakeProxyFn(fn func(*ProxyRequest)) *activatorTest {
	record := make([]*ProxyRequest, 0)
	at.fakeProxy = &fakeProxy{
		fn:     fn,
		record: record,
	}
	go func() {
		for {
			req := <-at.proxyRequests
			at.fakeProxy.record = append(at.fakeProxy.record, req)
			fn(req)
		}
	}()
	return at
}

func succeedingRevisionActivatorFn(release chan struct{}, ip string, port int32) func(*RevisionId) *RevisionEndpoint {
	return func(r *RevisionId) *RevisionEndpoint {
		_ = <-release
		return &RevisionEndpoint{
			RevisionId: *r,
			endpoint: endpoint{
				ip:   ip,
				port: port,
			},
		}
	}
}

func failingRevisionActivatorFn(err error, status int) func(*RevisionId) *RevisionEndpoint {
	return func(r *RevisionId) *RevisionEndpoint {
		return &RevisionEndpoint{
			RevisionId: *r,
			err:        err,
			status:     status,
		}
	}
}

func succeedingProxyFn() func(*ProxyRequest) {
	return func(_ *ProxyRequest) {}
}
