package activator

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/knative/serving/pkg/apis/serving"
	pkghttp "github.com/knative/serving/pkg/http"
	"go.uber.org/zap"
)

// Endpoint is a fully-qualified domain name / port pair for an active revision.
type Endpoint struct {
	FQDN string
	Port int32
}

type EndpointListener struct {
	mux     sync.Mutex
	Logger  *zap.SugaredLogger
	watches map[string]*endpointWatch
}

type endpointWatch struct {
	el        *EndpointListener
	bcastCh   chan struct{}
	namespace string
	revision  string
	refs      uint
	endpoint  *Endpoint
}

func flattenNamespaceRevision(namespace, revision string) string {
	return fmt.Sprintf("%s/%s", namespace, revision)
}

func NewEndpointListener(logger *zap.SugaredLogger) *EndpointListener {
	return &EndpointListener{
		Logger:  logger,
		watches: make(map[string]*endpointWatch),
	}
}

func (el *EndpointListener) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	namespace := pkghttp.LastHeaderValue(r.Header, serving.ActivatorRevisionHeaderNamespace)
	name := pkghttp.LastHeaderValue(r.Header, serving.ActivatorRevisionHeaderName)
	nsr := flattenNamespaceRevision(namespace, name)

	el.mux.Lock()
	defer el.mux.Unlock()

	if watch, ok := el.watches[nsr]; ok {
		watch.endpoint = &Endpoint{}
		close(watch.bcastCh)
	}
}

// TODO(greghaynes) We should add a timeout equal to our activator retry delay for the lifetime of an entire EndpointWatch
func (el *EndpointListener) WaitForEndpoint(namespace, revision string, closeCh <-chan struct{}) *Endpoint {
	nsr := flattenNamespaceRevision(namespace, revision)

	el.mux.Lock()
	if watch, ok := el.watches[nsr]; !ok {
		el.watches[nsr] = &endpointWatch{
			el:        el,
			bcastCh:   make(chan struct{}),
			namespace: namespace,
			revision:  revision,
			refs:      0,
		}
	} else {
		if watch.endpoint != nil {
			el.mux.Unlock()
			return watch.endpoint
		}
		watch.refs += 1
	}
	el.mux.Unlock()

	// Wait for bcast or close
	var ret *Endpoint
	ew := el.watches[nsr]
	select {
	case <-closeCh:
		break
	case <-ew.bcastCh:
		ret = ew.endpoint
	}

	el.mux.Lock()
	defer el.mux.Unlock()

	ew.refs -= 1
	if ew.refs == 0 {
		delete(el.watches, nsr)
	}

	return ret
}
