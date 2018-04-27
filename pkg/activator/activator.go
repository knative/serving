package activator

import (
	"log"
	"net/http"
)

// RevisionId is the name and namespace of a revision.
type RevisionId struct {
	name      string
	namespace string
}

func (r RevisionId) string() string {
	return r.namespace + "/" + r.name
}

// HttpRequest is a single http request which needs to be proxied to an
// active revision.
type HttpRequest struct {
	RevisionId
	w http.ResponseWriter
	r *http.Request
}

func NewHttpRequest(w http.ResponseWriter, r *http.Request, namespace, name string) *HttpRequest {
	return &HttpRequest{
		RevisionId: RevisionId{
			namespace: namespace,
			name:      name,
		},
		w: w,
		r: r,
	}
}

// Endpoint is an IP, port pair.
type endpoint struct {
	ip   string
	port int32
}

// RevisionEndpoint is the endpoint of an active revision. It will
// include either an endpoint or an error and error code identifying
// why an endpoint could not be obtained.
type RevisionEndpoint struct {
	RevisionId
	endpoint
	err    error
	status int
}

// ProxyRequest is a single http request which is ready to be proxied to
// an active revision.  The revision's endpoint is included.
type ProxyRequest struct {
	HttpRequest
	endpoint
}

// Activator is a component that proxies http requests to active
// revisions, making them active if necessary.  It outsources the
// work of activation to the RevisionActivator component after
// deduplicating requests.  It outsources the work of proxying
// requests to the Proxy component.
type Activator struct {
	pendingRequests    map[string][]*HttpRequest
	httpRequests       <-chan *HttpRequest
	activationRequests chan<- *RevisionId
	endpoints          <-chan *RevisionEndpoint
	proxyRequests      chan<- *ProxyRequest
}

func NewActivator(
	httpRequests <-chan *HttpRequest,
	activationRequests chan<- *RevisionId,
	endpoints <-chan *RevisionEndpoint,
	proxyRequests chan<- *ProxyRequest,
) *Activator {
	a := &Activator{
		pendingRequests:    make(map[string][]*HttpRequest),
		httpRequests:       httpRequests,
		activationRequests: activationRequests,
		endpoints:          endpoints,
		proxyRequests:      proxyRequests,
	}
	go func() {
		log.Println("Activator up.")
		for {
			select {
			case req := <-httpRequests:
				id := req.RevisionId.string()
				if reqs, ok := a.pendingRequests[id]; ok {
					a.pendingRequests[id] = append(reqs, req)
				} else {
					// First request for this revision
					a.pendingRequests[id] = []*HttpRequest{req}
					a.activationRequests <- &req.RevisionId
				}
			case end := <-endpoints:
				id := end.RevisionId.string()
				if reqs, ok := a.pendingRequests[id]; ok {
					for _, r := range reqs {
						// if end has error, write error and don't proxy
						pr := &ProxyRequest{
							HttpRequest: *r,
							endpoint:    end.endpoint,
						}
						go func() { proxyRequests <- pr }()
					}
				}
				delete(a.pendingRequests, id)
			}
		}
	}()
	return a
}
