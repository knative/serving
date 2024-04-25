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

// route.go provides methods to perform actions on the route resource.

package test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logging"
	"knative.dev/pkg/test/spoof"
)

// Prober is the interface for a prober, which checks the result of the probes when stopped.
type Prober interface {
	// SLI returns the "service level indicator" for the prober, which is the observed
	// success rate of the probes.  This will panic if the prober has not been stopped.
	SLI() (total int64, failures int64)

	// Stop terminates the prober, returning any observed errors.
	// Implementations may choose to put additional requirements on
	// the prober, which may cause this to block (e.g. a minimum number
	// of probes to achieve a population suitable for SLI measurement).
	Stop() error
}

type prober struct {
	// These shouldn't change after creation
	logf          logging.FormatLogger
	url           *url.URL
	minimumProbes int64

	requests atomic.Int64
	failures atomic.Int64

	// This channel is simply closed when minimumProbes has been satisfied.
	minDoneCh chan struct{}

	errGrp *errgroup.Group
	ctx    context.Context
	cancel context.CancelFunc
}

// prober implements Prober
var _ Prober = (*prober)(nil)

// SLI implements Prober
func (p *prober) SLI() (int64, int64) {
	return p.requests.Load(), p.failures.Load()
}

// Stop implements Prober
func (p *prober) Stop() error {
	// Wait for either an error to happen or the minimumProbes we want.
	select {
	case <-p.ctx.Done():
	case <-p.minDoneCh:
	}

	// Stop all probing.
	p.cancel()

	return p.errGrp.Wait()
}

// ProberManager is the interface for spawning probers, and checking their results.
type ProberManager interface {
	// The ProberManager should expose a way to collectively reason about spawned
	// probes as a sort of aggregating Prober.
	Prober

	// Spawn creates a new Prober
	Spawn(url *url.URL) Prober

	// Foreach iterates over the probers spawned by this ProberManager.
	Foreach(func(url *url.URL, p Prober))
}

type manager struct {
	// Should not change after creation
	logf      logging.FormatLogger
	clients   *Clients
	minProbes int64

	m                sync.RWMutex
	probes           map[*url.URL]Prober
	transportOptions []spoof.TransportOption
}

var _ ProberManager = (*manager)(nil)

// Spawn implements ProberManager
func (m *manager) Spawn(url *url.URL) Prober {
	m.m.Lock()
	defer m.m.Unlock()

	if p, ok := m.probes[url]; ok {
		return p
	}

	m.logf("Starting Route prober for %s.", url)

	ctx, cancel := context.WithCancel(context.Background())
	errGrp, ctx := errgroup.WithContext(ctx)

	p := &prober{
		logf:          m.logf,
		url:           url,
		minimumProbes: m.minProbes,

		minDoneCh: make(chan struct{}),

		errGrp: errGrp,
		ctx:    ctx,
		cancel: cancel,
	}
	m.probes[url] = p

	errGrp.Go(func() error {
		client, err := pkgTest.NewSpoofingClient(ctx, m.clients.KubeClient, m.logf, url.Hostname(), NetworkingFlags.ResolvableDomain, m.transportOptions...)
		if err != nil {
			return fmt.Errorf("failed to generate client: %w", err)
		}

		req, err := http.NewRequest(http.MethodGet, url.String(), nil)
		if err != nil {
			return fmt.Errorf("failed to generate request: %w", err)
		}

		// We keep polling the domain and accumulate success rates
		// to ultimately establish the SLI and compare to the SLO.
		for {
			select {
			case <-ctx.Done():
				return nil
			default:
				res, err := client.Do(req)
				if p.requests.Inc() == p.minimumProbes {
					close(p.minDoneCh)
				}
				if err != nil {
					p.logf("%q error: %v", p.url, err)
					p.failures.Inc()
				} else if res.StatusCode != http.StatusOK {
					p.logf("%q status = %d, want: %d", p.url, res.StatusCode, http.StatusOK)
					p.logf("Response: %s", res)
					p.failures.Inc()
				}
			}
		}
	})
	return p
}

// Stop implements ProberManager
func (m *manager) Stop() error {
	m.m.Lock()
	defer m.m.Unlock()

	m.logf("Stopping all probers")

	errgrp := &errgroup.Group{}
	for _, prober := range m.probes {
		errgrp.Go(prober.Stop)
	}
	return errgrp.Wait()
}

// SLI implements Prober
func (m *manager) SLI() (total int64, failures int64) {
	m.m.RLock()
	defer m.m.RUnlock()
	for _, prober := range m.probes {
		pt, pf := prober.SLI()
		total += pt
		failures += pf
	}
	return
}

// Foreach implements ProberManager
func (m *manager) Foreach(f func(url *url.URL, p Prober)) {
	m.m.RLock()
	defer m.m.RUnlock()

	for url, prober := range m.probes {
		f(url, prober)
	}
}

// NewProberManager creates a new manager for probes.
func NewProberManager(logf logging.FormatLogger, clients *Clients, minProbes int64, opts ...spoof.TransportOption) ProberManager {
	return &manager{
		logf:             logf,
		clients:          clients,
		minProbes:        minProbes,
		probes:           make(map[*url.URL]Prober),
		transportOptions: opts,
	}
}

// RunRouteProber starts a single Prober of the given domain.
func RunRouteProber(logf logging.FormatLogger, clients *Clients, url *url.URL, opts ...spoof.TransportOption) Prober {
	// Default to 10 probes
	pm := NewProberManager(logf, clients, 10, opts...)
	pm.Spawn(url)
	return pm
}

// AssertProberDefault is a helper for stopping the Prober and checking its SLI
// against the default SLO, which requires perfect responses.
// This takes `testing.T` so that it may be used in `defer`.
func AssertProberDefault(t testing.TB, p Prober) {
	t.Helper()
	if err := p.Stop(); err != nil {
		t.Error("Stop()", "error", err.Error())
	}
	// Default to 100% correct (typically used in conjunction with the low probe count above)
	if err := CheckSLO(1.0, t.Name(), p); err != nil {
		t.Error("CheckSLO()", "error", err.Error())
	}
}

// CheckSLO compares the SLI of the given prober against the SLO, erroring if too low.
func CheckSLO(slo float64, name string, p Prober) error {
	total, failures := p.SLI()

	successRate := float64(total-failures) / float64(total)
	if successRate < slo {
		return fmt.Errorf("SLI for %q = %f, wanted >= %f", name, successRate, slo)
	}
	return nil
}
