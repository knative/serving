/*
Copyright 2018 The Knative Authors

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
	"sync"
	"testing"
	"time"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/pkg/test/spoof"
	"golang.org/x/sync/errgroup"
)

// Prober is the interface for a prober, which checks the result of the probes when stopped.
type Prober interface {
	// SLI returns the "service level indicator" for the prober, which is the observed
	// success rate of the probes.  This will panic if the prober has not been stopped.
	SLI() (total int64, failures int64)

	// Stop terminates the prober, returning any observed errors.
	Stop() error
}

type prober struct {
	// These shouldn't change after creation
	logger        *logging.BaseLogger
	domain        string
	minimumProbes int64

	// m guards access to these fields
	m        sync.Mutex
	requests int64
	failures int64
	stopped  bool
	err      error
}

// prober implements Prober
var _ Prober = (*prober)(nil)

// SLI implements Prober
func (p *prober) SLI() (int64, int64) {
	p.m.Lock()
	defer p.m.Unlock()

	return p.requests, p.failures
}

// Stop implements Prober
func (p *prober) Stop() error {
	for {
		if done, err := p.isDone(); err != nil {
			return err
		} else if done {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
}

func (p *prober) isDone() (bool, error) {
	p.m.Lock()
	defer p.m.Unlock()

	if p.err != nil {
		p.stopped = true
		return true, p.err
	}

	if p.requests < p.minimumProbes {
		p.logger.Infof("%q needs more probes; got %d, want %d", p.domain, p.requests, p.minimumProbes)
		return false, nil
	}

	p.stopped = true
	return true, nil
}

func (p *prober) handleError(err error) {
	p.m.Lock()
	defer p.m.Unlock()

	p.err = err
}

func (p *prober) handleResponse(response *spoof.Response) (bool, error) {
	p.m.Lock()
	defer p.m.Unlock()

	if p.stopped {
		return p.stopped, nil
	}

	p.requests++
	if response.StatusCode != http.StatusOK {
		p.logger.Infof("%q got bad status: %d\nHeaders:%v\nBody: %s", p.domain, response.StatusCode,
			response.Header, string(response.Body))
		p.failures++
	}

	// Returning (false, nil) causes SpoofingClient.Poll to retry.
	// sc.logger.Infof("Retrying for code %v", resp.StatusCode)
	return p.stopped, nil
}

// ProberManager is the interface for spawning probers, and checking their results.
type ProberManager interface {
	// Spawn creates a new Prober
	Spawn(domain string) Prober

	// StopAll terminates all probers, returning any observed errors.
	StopAll() error

	// Check compares each of the prober SLIs against the localSLO and
	// then the aggregate of the SLIs against the globalSLO
	Check(t *testing.T, localSLO, globalSLO float64)
}

type manager struct {
	// Should not change after creation
	logger    *logging.BaseLogger
	clients   *Clients
	minProbes int64

	m      sync.Mutex
	probes map[string]Prober
}

var _ ProberManager = (*manager)(nil)

// Spawn implements ProberManager
func (m *manager) Spawn(domain string) Prober {
	m.m.Lock()
	defer m.m.Unlock()

	if p, ok := m.probes[domain]; ok {
		return p
	}

	m.logger.Infof("Starting Route prober for route domain %s.", domain)
	p := &prober{logger: m.logger, domain: domain, minimumProbes: m.minProbes}
	m.probes[domain] = p
	go func() {
		client, err := pkgTest.NewSpoofingClient(m.clients.KubeClient, m.logger, domain,
			ServingFlags.ResolvableDomain)
		if err != nil {
			m.logger.Infof("NewSpoofingClient() = %v", err)
			p.handleError(err)
			return
		}

		// RequestTimeout is set to 0 to make the polling infinite.
		client.RequestTimeout = 0 * time.Minute
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", domain), nil)
		if err != nil {
			m.logger.Infof("NewRequest() = %v", err)
			p.handleError(err)
			return
		}

		// We keep polling Route if the response status is OK.
		// If the response status is not OK, we stop the prober and
		// generate error based on the response.z
		_, err = client.Poll(req, p.handleResponse)
		if err != nil {
			m.logger.Infof("Poll() = %v", err)
			p.handleError(err)
			return
		}
	}()
	return p
}

// StopAll implements ProberManager
func (m *manager) StopAll() error {
	m.m.Lock()
	defer m.m.Unlock()

	m.logger.Info("Stopping all probers")

	errgrp := &errgroup.Group{}
	for _, prober := range m.probes {
		errgrp.Go(prober.Stop)
	}
	return errgrp.Wait()
}

// Check implements ProberManager
func (m *manager) Check(t *testing.T, localSLO, globalSLO float64) {
	m.m.Lock()
	defer m.m.Unlock()

	m.logger.Infof("Checking local (%f) and global (%v) SLOs", localSLO, globalSLO)

	var globalTotal, globalFailures int64
	for domain, prober := range m.probes {
		total, failures := prober.SLI()

		successRate := float64(total-failures) / float64(total)
		m.logger.Infof("%q SLI is %f (total: %d, failures: %d)", domain, successRate, total, failures)
		if successRate < localSLO {
			t.Errorf("SLI for %q = %f, wanted >= %f", domain, successRate, localSLO)
		}
		globalTotal += total
		globalFailures += failures
	}

	globalSuccessRate := float64(globalTotal-globalFailures) / float64(globalTotal)
	m.logger.Infof("global SLI is %f (total: %d, failures: %d)", globalSuccessRate, globalTotal, globalFailures)
	if globalSuccessRate < globalSLO {
		t.Errorf("SLI for %q = %f, wanted >= %f", "all", globalSuccessRate, globalSLO)
	}
}

func NewProberManager(logger *logging.BaseLogger, clients *Clients, minProbes int64) ProberManager {
	return &manager{
		logger:    logger,
		clients:   clients,
		minProbes: minProbes,
		probes:    make(map[string]Prober),
	}
}

func RunRouteProber(logger *logging.BaseLogger, clients *Clients, domain string) ProberManager {
	// Default to 10 probes
	pm := NewProberManager(logger, clients, 10)
	pm.Spawn(domain)
	return pm
}

func CheckProberDefault(t *testing.T, pm ProberManager) {
	if err := pm.StopAll(); err != nil {
		t.Errorf("StopAll() = %v", err)
	}
	// Default to 100% correct (typically used in conjunctions with the low probe count above)
	pm.Check(t, 1.0, 1.0)
}
