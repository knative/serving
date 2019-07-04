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

	"github.com/knative/serving/pkg/queue/health"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	kProbeTimeout = 100 * time.Millisecond
	// Set equal to the queue-proxy's ExecProbe timeout to take
	// advantage of the full window
	PollTimeout   = 10 * time.Second
	retryInterval = 50 * time.Millisecond
)

// Probe wraps a corev1.Probe along with a logger and a count of consecutive, successful probes
type Probe struct {
	*corev1.Probe
	count  int32
	logger *zap.SugaredLogger
}

// NewProbe returns a pointer a new Probe
func NewProbe(v1p *corev1.Probe, logger *zap.SugaredLogger) *Probe {
	return &Probe{
		Probe:  v1p,
		logger: logger,
	}
}

// IsKProbe indicates whether the default Knative probe with aggresive retries should be used.
func (p *Probe) IsKProbe() bool {
	return p.PeriodSeconds == 0
}

// ProbeContainer executes the defined Probe against the user-container
func (p *Probe) ProbeContainer() bool {
	var err error

	if p.HTTPGet != nil {
		err = p.httpProbe()
	} else if p.TCPSocket != nil {
		err = p.tcpProbe()
	} else if p.Exec != nil {
		// Assumes the true readinessProbe is being executed directly on user container. See (#4086).
		return true
	} else {
		// using Fprintf for a concise error message in the event log
		fmt.Fprintf(os.Stderr, "unimplemented probe type.")
		return false
	}

	if err != nil {
		p.logger.Errorw("User-container could not be probed successfully.", zap.Error(err))
		return false
	}

	p.logger.Info("User-container successfully probed.")
	return true
}

func (p *Probe) timeout() time.Duration {
	if p.IsKProbe() {
		return kProbeTimeout
	}
	return time.Duration(p.TimeoutSeconds) * time.Second
}

// tcpProbe function executes TCP probe once if its standard probe
// otherwise TCP probe polls condition function which returns true
// if the probe count is greater than success threshold and false if TCP probe fails
func (p *Probe) tcpProbe() error {
	config := health.TCPProbeConfigOptions{
		Address:       fmt.Sprintf("%s:%d", p.TCPSocket.Host, p.TCPSocket.Port.IntValue()),
		SocketTimeout: p.timeout(),
	}
	if !p.IsKProbe() {
		return health.TCPProbe(config)
	}

	return wait.PollImmediate(retryInterval, PollTimeout, func() (bool, error) {
		if tcpErr := health.TCPProbe(config); tcpErr != nil {
			p.count = 0
			return false, nil
		}
		p.count++
		return p.Count() >= p.SuccessThreshold, nil
	})
}

// httpProbe function executes HTTP probe once if its standard probe
// otherwise HTTP probe polls condition function which returns true
// if the probe count is greater than success threshold and false if HTTP probe fails
func (p *Probe) httpProbe() error {
	httpProbeConfig := health.HTTPProbeConfigOptions{
		HTTPGetAction: p.HTTPGet,
		Timeout:       p.timeout(),
	}
	if !p.IsKProbe() {
		return health.HTTPProbe(httpProbeConfig)
	}

	return wait.PollImmediate(retryInterval, PollTimeout, func() (bool, error) {
		if err := health.HTTPProbe(httpProbeConfig); err != nil {
			p.count = 0
			return false, nil
		}
		p.count++

		return p.Count() >= p.SuccessThreshold, nil
	})
}

// Count function fetches current probe count
func (p *Probe) Count() int32 {
	return p.count
}
