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
	count int32
}

// NewProbe returns a pointer a new Probe
func NewProbe(v1p *corev1.Probe) *Probe {
	return &Probe{
		Probe: v1p,
	}
}

// IsAggressive indicates whether the Knative probe with aggressive retries should be used.
func (p *Probe) IsAggressive() bool {
	return p.PeriodSeconds == 0
}

// ProbeContainer executes the defined Probe against the user-container
func (p *Probe) ProbeContainer() bool {
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
		fmt.Fprint(os.Stderr, err.Error())
		return false
	}
	return true
}

func (p *Probe) doProbe(probe func(time.Duration) error) error {
	if p.IsAggressive() {
		return wait.PollImmediate(retryInterval, PollTimeout, func() (bool, error) {
			if tcpErr := probe(aggressiveProbeTimeout); tcpErr != nil {
				// reset count of consecutive successes to zero
				p.count = 0
				return false, nil
			}

			p.count++

			// return success if count of consecutive successes is equal to or greater
			// than the probe's SuccessThreshold.
			return p.Count() >= p.SuccessThreshold, nil
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

// Count function fetches current probe count
func (p *Probe) Count() int32 {
	return p.count
}
