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

// Endpoint is an IP, port pair.
type endpoint struct {
	ip   string
	port string
}

// RevisionEndpoint is the endpoint of an active revision. It will
// include either an Endpoint or an error and error code.
type RevisionEndpoint struct {
	revisionId
	endpoint
	err    error
	status int
}

// ProxyRequest is a single http request which is ready to be proxied to
// an active revision.  The Ip address of the active revision's endpoint
// is included.
type ProxyRequest struct {
	HttpRequest
	endpoint
}

// Activator is a component that proxies http requests to active
// revisions, making them active if necessary.
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
					d.pendingRequests[id] = []*HttpRequest{req}
				}
				d.activationRequests <- req.RevisionId
			case end <- activeEndpoints:
				id := end.RevisionId.String()
				if reqs, ok := d.pendingRequests[id]; ok {
					for _, r := range reqs {
						pr := &ProxyRequest{
							HttpRequest: r,
							Endpoint:    end.Endpoint,
						}
						go func() { proxyRequests <- pr }()
					}
				}
			}
		}
	}()
	return a
}
