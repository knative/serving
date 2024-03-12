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
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
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

// Probe holds all wrapped *corev1.Probe along with a barrier to sync single probing execution
type Probe struct {
	probes []*wrappedProbe
	out    io.Writer // To make tests not log errors in good cases.

	// Barrier sync to ensure only one probe is happening at the same time.
	// When a probe is active `gv` will be non-nil.
	// When the probe finishes the `gv` will be reset to nil.
	mu sync.RWMutex
	gv *gateValue
}

type wrappedProbe struct {
	*corev1.Probe
	count           int32
	pollTimeout     time.Duration // To make tests not run for 10 seconds.
	out             io.Writer     // To make tests not log errors in good cases.
	autoDetectHTTP2 bool          // Feature gate to enable HTTP2 auto-detection.
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

// NewProbe returns a pointer to a new Probe.
func NewProbe(probes []*corev1.Probe) *Probe {
	wrappedProbes := []*wrappedProbe{}
	for _, p := range probes {
		wrappedProbes = append(wrappedProbes, &wrappedProbe{
			Probe:       p,
			out:         os.Stderr,
			pollTimeout: PollTimeout,
		})
	}
	return &Probe{
		probes: wrappedProbes,
		out:    os.Stderr,
	}
}

// NewProbeWithHTTP2AutoDetection returns a pointer to a new Probe that has HTTP2
// auto-detection enabled.
func NewProbeWithHTTP2AutoDetection(probes []*corev1.Probe) *Probe {
	wrappedProbes := []*wrappedProbe{}
	for _, p := range probes {
		wrappedProbes = append(wrappedProbes, &wrappedProbe{
			Probe:           p,
			out:             os.Stderr,
			pollTimeout:     PollTimeout,
			autoDetectHTTP2: true,
		})
	}
	return &Probe{
		probes: wrappedProbes,
		out:    os.Stderr,
	}
}

// shouldProbeAggressively indicates whether the Knative probe with aggressive retries should be used.
func (p *wrappedProbe) shouldProbeAggressively() bool {
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
	var probeGroup errgroup.Group

	for _, probe := range p.probes {
		innerProbe := probe
		probeGroup.Go(func() error {
			switch {
			case innerProbe.HTTPGet != nil:
				return innerProbe.httpProbe()
			case innerProbe.TCPSocket != nil:

				return innerProbe.tcpProbe()
			case innerProbe.GRPC != nil:
				return innerProbe.grpcProbe()
			case innerProbe.Exec != nil:
				// Should never be reachable. Exec probes to be translated to
				// TCP probes when container is built.
				return fmt.Errorf("exec probe not supported")
			default:
				return fmt.Errorf("no probe found")
			}
		})
	}

	err := probeGroup.Wait()
	if err != nil {
		fmt.Fprintln(p.out, err.Error())
		return false
	}

	return true
}

func (p *wrappedProbe) doProbe(probe func(time.Duration) error) error {
	if !p.shouldProbeAggressively() {
		return probe(time.Duration(p.TimeoutSeconds) * time.Second)
	}

	var failCount int
	var lastProbeErr error
	pollErr := wait.PollUntilContextTimeout(context.Background(), retryInterval, p.pollTimeout, true, func(context.Context) (bool, error) {
		if err := probe(aggressiveProbeTimeout); err != nil {
			// Reset count of consecutive successes to zero.
			p.count = 0
			// Don't log this now since we probe every 50ms and some failures are
			// expected if the user container takes longer than that to start up.
			// We'll log the lastProbeErr if we don't eventually succeed.
			lastProbeErr = err
			failCount++
			return false, nil
		}

		p.count++

		// Return success if count of consecutive successes is equal to or greater
		// than the probe's SuccessThreshold.
		return p.count >= p.SuccessThreshold, nil
	})

	if pollErr != nil && lastProbeErr != nil {
		fmt.Fprintf(p.out, "aggressive probe error (failed %d times): %v\n", failCount, lastProbeErr)
	}

	return pollErr
}

// tcpProbe function executes TCP probe once if its standard probe
// otherwise TCP probe polls condition function which returns true
// if the probe count is greater than success threshold and false if TCP probe fails
func (p *wrappedProbe) tcpProbe() error {
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
func (p *wrappedProbe) httpProbe() error {
	config := health.HTTPProbeConfigOptions{
		HTTPGetAction: p.HTTPGet,
		MaxProtoMajor: 1,
	}
	if p.autoDetectHTTP2 {
		// A value of 0 indicates that the prober should find out which version is
		// supported.
		config.MaxProtoMajor = 0
	}

	return p.doProbe(func(to time.Duration) error {
		config.Timeout = to
		return health.HTTPProbe(config)
	})
}

// grpcProbe function executes gRPC probe
func (p *wrappedProbe) grpcProbe() error {
	config := health.GRPCProbeConfigOptions{
		GRPCAction: p.GRPC,
	}

	return p.doProbe(func(to time.Duration) error {
		config.Timeout = to
		return health.GRPCProbe(config)
	})
}
