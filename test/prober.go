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
	"fmt"
	"net/http"
	"net/url"
	"sync"

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

	// m guards access to these fields
	m        sync.RWMutex
	requests int64
	failures int64
	stopped  bool

	// This channel is used to send errors encountered probing the domain.
	errCh chan error
	// This channel is simply closed when minimumProbes has been satisfied.
	minDoneCh chan struct{}
}

// prober implements Prober
var _ Prober = (*prober)(nil)

// SLI implements Prober
func (p *prober) SLI() (int64, int64) {
	p.m.RLock()
	defer p.m.RUnlock()

	return p.requests, p.failures
}

// Stop implements Prober
func (p *prober) Stop() error {
	// When we're done stop sending requests.
	defer func() {
		p.m.Lock()
		defer p.m.Unlock()
		p.stopped = true
	}()

	// Check for any immediately available errors
	select {
	case err := <-p.errCh:
		return err
	default:
		// Don't block if there are no errors immediately available.
	}

	// If there aren't any immediately available errors, then
	// wait for either an error or the minimum number of probes
	// to be satisfied.
	select {
	case err := <-p.errCh:
		return err
	case <-p.minDoneCh:
		return nil
	}
}

func (p *prober) handleResponse(response *spoof.Response) (bool, error) {
	p.m.Lock()
	defer p.m.Unlock()

	if p.stopped {
		return p.stopped, nil
	}

	p.logRequestNoLock()
	if response.StatusCode != http.StatusOK {
		p.logf("%q status = %d, want: %d", p.url, response.StatusCode, http.StatusOK)
		p.logf("response: %s", response)
		p.failures++
	}

	// Returning (false, nil) causes SpoofingClient.Poll to retry.
	return false, nil
}

func (p *prober) handleErrorRetry(err error) (bool, error) {
	p.m.Lock()
	defer p.m.Unlock()

	p.logRequestNoLock()
	p.failures++

	// Returning true causes SpoofingClient.Poll to retry.
	return true, fmt.Errorf("retry on all errors: %v", err)
}

// logRequestNoLock should always be called after obtaining p.m.Lock(),
// thus it doesn't try to get the lock here again.
func (p *prober) logRequestNoLock() {
	p.requests++
	if p.requests == p.minimumProbes {
		close(p.minDoneCh)
	}
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

	m      sync.RWMutex
	probes map[*url.URL]Prober
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
	p := &prober{
		logf:          m.logf,
		url:           url,
		minimumProbes: m.minProbes,
		errCh:         make(chan error, 1),
		minDoneCh:     make(chan struct{}),
	}
	m.probes[url] = p
	go func() {
		client, err := pkgTest.NewSpoofingClient(m.clients.KubeClient, m.logf, url.Hostname(), ServingFlags.ResolvableDomain)
		if err != nil {
			m.logf("NewSpoofingClient() = %v", err)
			p.errCh <- err
			return
		}

		// RequestTimeout is set to 0 to make the polling infinite.
		client.RequestTimeout = 0
		req, err := http.NewRequest(http.MethodGet, url.String(), nil)
		if err != nil {
			m.logf("NewRequest() = %v", err)
			p.errCh <- err
			return
		}

		// We keep polling the domain and accumulate success rates
		// to ultimately establish the SLI and compare to the SLO.
		_, err = client.Poll(req, p.handleResponse, p.handleErrorRetry)
		if err != nil {
			// SLO violations are not reflected as errors. They are
			// captured and calculated internally.
			m.logf("Poll() = %v", err)
			p.errCh <- err
			return
		}
	}()
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
func NewProberManager(logf logging.FormatLogger, clients *Clients, minProbes int64) ProberManager {
	return &manager{
		logf:      logf,
		clients:   clients,
		minProbes: minProbes,
		probes:    make(map[*url.URL]Prober),
	}
}

// RunRouteProber starts a single Prober of the given domain.
func RunRouteProber(logf logging.FormatLogger, clients *Clients, url *url.URL) Prober {
	// Default to 10 probes
	pm := NewProberManager(logf, clients, 10)
	pm.Spawn(url)
	return pm
}

// AssertProberDefault is a helper for stopping the Prober and checking its SLI
// against the default SLO, which requires perfect responses.
// This takes `testing.T` so that it may be used in `defer`.
func AssertProberDefault(t pkgTest.T, p Prober) {
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
