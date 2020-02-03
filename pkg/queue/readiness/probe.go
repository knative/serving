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

package readiness

import (
	"fmt"
	"os"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/serving/pkg/queue/health"
)

const (
	aggressiveProbeTimeout = 100 * time.Millisecond
	// PollTimeout is set equal to the queue-proxy's ExecProbe timeout to take
	// advantage of the full window
	PollTimeout   = 10 * time.Second
	retryInterval = 50 * time.Millisecond
)

// Probe wraps a corev1.Probe along with a count of consecutive, successful probes
type Probe struct {
	*corev1.Probe
	count       int32
	pollTimeout time.Duration // To make tests not run for 10 seconds.

	// Barrier sync to ensure only one probe is happening at the same time.
	// When a probe is active `gv` will be non-nil.
	// When the probe finishes the `gv` will be reset to nil.
	mu sync.RWMutex
	gv *gateValue
}

// gateValue is a write-once boolean impl.
type gateValue struct {
	broadcast chan struct{}
	result    bool
}

// newGV returns a gateValue which is ready to write.
func newGV() *gateValue {
	return &gateValue{
		broadcast: make(chan struct{}),
	}
}

// only `writer` must call `write` to set the value.
// `write` will panic if called more than once.
func (gv *gateValue) write(val bool) {
	gv.result = val
	close(gv.broadcast)
}

// `read` can be called multiple times.
func (gv *gateValue) read() bool {
	<-gv.broadcast
	return gv.result
}

// NewProbe returns a pointer a new Probe
func NewProbe(v1p *corev1.Probe) *Probe {
	return &Probe{
		Probe:       v1p,
		pollTimeout: PollTimeout,
	}
}

// IsAggressive indicates whether the Knative probe with aggressive retries should be used.
func (p *Probe) IsAggressive() bool {
	return p.PeriodSeconds == 0
}

// ProbeContainer executes the defined Probe against the user-container
func (p *Probe) ProbeContainer() bool {
	gv, writer := func() (*gateValue, bool) {
		p.mu.Lock()
		defer p.mu.Unlock()
		// If gateValue exists (i.e. right now something is probing, attach to that probe).
		if p.gv != nil {
			return p.gv, false
		}
		p.gv = newGV()
		return p.gv, true
	}()

	if writer {
		res := p.probeContainerImpl()
		gv.write(res)
		p.mu.Lock()
		defer p.mu.Unlock()
		p.gv = nil
		return res
	}
	return gv.read()
}

func (p *Probe) probeContainerImpl() bool {
	var err error

	switch {
	case p.HTTPGet != nil:
		err = p.httpProbe()
	case p.TCPSocket != nil:
		err = p.tcpProbe()
	case p.Exec != nil:
		// Should never be reachable. Exec probes to be translated to
		// TCP probes when container is built.
		// Using Fprintf for a concise error message in the event log.
		fmt.Fprintln(os.Stderr, "exec probe not supported")
		return false
	default:
		// Using Fprintf for a concise error message in the event log.
		fmt.Fprintln(os.Stderr, "no probe found")
		return false
	}

	if err != nil {
		// Using Fprintf for a concise error message in the event log.
		fmt.Fprintln(os.Stderr, err.Error())
		return false
	}
	return true
}

func (p *Probe) doProbe(probe func(time.Duration) error) error {
	if p.IsAggressive() {
		return wait.PollImmediate(retryInterval, p.pollTimeout, func() (bool, error) {
			if err := probe(aggressiveProbeTimeout); err != nil {
				fmt.Fprintln(os.Stderr, "aggressive probe error: ", err)
				// Reset count of consecutive successes to zero.
				p.count = 0
				return false, nil
			}

			p.count++

			// Return success if count of consecutive successes is equal to or greater
			// than the probe's SuccessThreshold.
			return p.count >= p.SuccessThreshold, nil
		})
	}

	return probe(time.Duration(p.TimeoutSeconds) * time.Second)
}

// tcpProbe function executes TCP probe once if its standard probe
// otherwise TCP probe polls condition function which returns true
// if the probe count is greater than success threshold and false if TCP probe fails
func (p *Probe) tcpProbe() error {
	config := health.TCPProbeConfigOptions{
		Address: p.TCPSocket.Host + ":" + p.TCPSocket.Port.String(),
	}

	return p.doProbe(func(to time.Duration) error {
		config.SocketTimeout = to
		return health.TCPProbe(config)
	})
}

// httpProbe function executes HTTP probe once if its standard probe
// otherwise HTTP probe polls condition function which returns true
// if the probe count is greater than success threshold and false if HTTP probe fails
func (p *Probe) httpProbe() error {
	config := health.HTTPProbeConfigOptions{
		HTTPGetAction: p.HTTPGet,
	}

	return p.doProbe(func(to time.Duration) error {
		config.Timeout = to
		return health.HTTPProbe(config)
	})
}
