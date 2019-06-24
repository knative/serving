/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package prober

import (
	"context"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/knative/serving/pkg/network"
)

// TransportFactory is a function which returns an HTTP transport.
type TransportFactory func() http.RoundTripper

// ProbeOption is a way for caller to modify the HTTP request before it goes out.
type ProbeOption func(r *http.Request) *http.Request

// Do sends a single probe to given target, e.g. `http://revision.default.svc.cluster.local:81`.
// headerValue is the value for the `k-network-probe` header.
// Do returns whether the probe was successful or not, or there was an error probing.
func Do(ctx context.Context, transport http.RoundTripper, target, headerValue string, pos ...ProbeOption) (bool, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return false, errors.Wrapf(err, "%s is not a valid URL", target)
	}
	for _, po := range pos {
		req = po(req)
	}

	req.Header.Set(network.ProbeHeaderName, headerValue)
	req = req.WithContext(ctx)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		return false, errors.Wrapf(err, "error roundtripping %s", target)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, errors.Wrap(err, "error reading body")
	}
	return resp.StatusCode == http.StatusOK && string(body) == headerValue, nil
}

// Done is a callback that is executed when the async probe has finished.
// `arg` is given by the caller at the offering time, while `success` and `err`
// are the return values of the `Do` call.
// It is assumed that the opaque arg is consistent for a given target and
// we will coalesce concurrent Offer invocations on target.
type Done func(arg interface{}, success bool, err error)

// Manager manages async probes and makes sure we run concurrently only a single
// probe for the same key.
type Manager struct {
	cb Done
	// NB: it is paramount to use factory here, since we need a fresh roundtripper
	// for every request. The way K8s Services work is that they won't terminate
	// the TCP connection while the backend is still alive, and we won't scale it
	// to zero, until we receive an activator response. Without mesh the connection
	// is never severed and we continue probing the pod.
	transportFactory TransportFactory

	// mu guards keys.
	mu   sync.Mutex
	keys sets.String
}

// New creates a new Manager, that will invoke the given callback when
// async probing is finished.
func New(cb Done, transportFactory TransportFactory) *Manager {
	return &Manager{
		keys:             sets.NewString(),
		cb:               cb,
		transportFactory: transportFactory,
	}
}

// Offer executes asynchronous probe using `target` as the key.
// If a probe with the same key already exists, Offer will return false and the
// call is discarded. If the request is accepted, Offer returns true.
// Otherwise Offer starts a goroutine that periodically executes
// `Do`, until timeout is reached, the probe succeeds, or fails with an error.
// In the end the callback is invoked with the provided `arg` and probing results.
func (m *Manager) Offer(ctx context.Context, target, headerValue string, arg interface{}, period, timeout time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.keys.Has(target) {
		return false
	}
	m.keys.Insert(target)
	m.doAsync(ctx, m.transportFactory, target, headerValue, arg, period, timeout)
	return true
}

// doAsync starts a go routine that probes the target with given period.
func (m *Manager) doAsync(ctx context.Context, transportFactory TransportFactory, target, headerValue string, arg interface{}, period, timeout time.Duration) {
	go func() {
		defer func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.keys.Delete(target)
		}()
		var (
			result bool
			err    error
		)
		err = wait.PollImmediate(period, timeout, func() (bool, error) {
			result, err = Do(ctx, transportFactory(), target, headerValue)
			return result, err
		})
		m.cb(arg, result, err)
	}()
}
