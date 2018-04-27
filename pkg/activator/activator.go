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

func (r RevisionId) string() {
	return Namespace + "/" + Name
}

// HttpRequest is a single http request which needs to be proxied to an
// active revision.
type HttpRequest struct {
	revisionId
	w http.ResponseWriter
	r *http.Request
}

func NewHttpRequest(w http.ResponseWriter, r *http.Request, namespace, name string) *HttpRequest {
	return &HttpRequest{
		revisionId: revisionId{
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
	port string
}

// RevisionEndpoint is the endpoint of an active revision. It will
// include either an endpoint or an error and error code identifying
// why an endpoint could not be obtained.
type RevisionEndpoint struct {
	revisionId
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
	httpRequests <-chan *Request,
	activationRequests chan<- *ActivationRequest,
	endpoints <-chan *RevisionEndpoint,
	proxyRequests <-chan *Request,
) *Activator {
	a := &Activator{
		pendingRequests:    make(map[string][]Request),
		httpRequests:       httpRequests,
		activationRequests: activationRequests,
		endpoints:          endpoints,
		proxyRequests:      proxyRequests,
	}
	go func() {
		log.Println("Activator up.")
		for {
			select {
			case req <- httpRequests:
				id := req.RevisionId.String()
				if reqs, ok := d.pendingRequests[id]; ok {
					d.pendingRequests[id] = append(d.pendingRequests[id], req)
				} else {
					// First request for this revision
					d.pendingRequests[id] = []*HttpRequest{req}
					d.activationRequests <- req.RevisionId
				}
			case end <- activeEndpoints:
				id := end.RevisionId.String()
				if reqs, ok := d.pendingRequests[id]; ok {
					for _, r := range reqs {
						// if end has error, write error and don't proxy
						pr := &ProxyRequest{
							HttpRequest: r,
							Endpoint:    end.Endpoint,
						}
						go func() { proxyRequests <- pr }()
					}
				}
				delete(d.pendingRequests, id)
			}
		}
	}()
	return a
}
