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
package activator

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"go.uber.org/zap"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	maxRetry              = 60
	defaultPeriodSeconds  = int32(1 * time.Second)
	defaultTimeoutSeconds = int32(1 * time.Second)
)

func verifyRevisionRoutability(revision *v1alpha1.Revision, endpoint *Endpoint, logger *zap.SugaredLogger) {
	// Proceed only if user has a HTTPGet readinessProbe defined
	if revision.Spec.Container.ReadinessProbe == nil ||
		revision.Spec.Container.ReadinessProbe.HTTPGet == nil {
		endpoint.Verified = Unknown
		return
	}

	endpoint.Verified = Fail
	probe := createHttpGetProbe(revision, *endpoint)

	// Number of seconds after the readiness probes are initiated
	time.Sleep(time.Second * int32ToDuration(probe.InitialDelaySeconds))

	retryCount := 1
	retryInterval := time.Second * int32ToDuration(probe.PeriodSeconds)

	for retryCount = 1; retryCount < maxRetry; retryCount++ {
		ready, err := checkHttpGetProbe(probe, logger)
		if err != nil {
			logger.Errorf("error checking probe", zap.String("error", err.Error()))
		}

		if ready {
			endpoint.Verified = Pass
			break
		}

		// How often (in seconds) to perform the probe
		time.Sleep(retryInterval)
	}

	logger.Infof("took %d probe retries for readiness", retryCount, zap.Any("endpoint", endpoint))
}

// Function creates HTTP readiness probe for revision
func createHttpGetProbe(revision *v1alpha1.Revision, endpoint Endpoint) *v1.Probe {
	probe := revision.Spec.Container.ReadinessProbe.DeepCopy()
	probe.HTTPGet.Scheme = "http"
	probe.HTTPGet.Host = endpoint.FQDN
	probe.HTTPGet.Port.Type = intstr.Int
	probe.HTTPGet.Port.IntVal = endpoint.Port

	if probe.TimeoutSeconds == 0 {
		probe.TimeoutSeconds = defaultTimeoutSeconds
	}
	if probe.PeriodSeconds == 0 {
		probe.PeriodSeconds = defaultPeriodSeconds
	}

	return probe
}

func checkHttpGetProbe(probe *v1.Probe, logger *zap.SugaredLogger) (ready bool, err error) {
	if probe == nil {
		return false, errors.New("probe cannot be nil")
	}

	if probe.HTTPGet == nil {
		return false, errors.New("probe HTTPGet cannot be nil")
	}

	host := fmt.Sprintf("%s:%d", probe.HTTPGet.Host, probe.HTTPGet.Port.IntVal)
	if err != nil {
		return false, err
	}

	probeUrl := url.URL{
		Scheme: string(probe.HTTPGet.Scheme),
		Host:   host,
		Path:   probe.HTTPGet.Path,
	}

	logger.Debug("checking probe url: %s", probeUrl.String())

	client := http.Client{Timeout: time.Second * int32ToDuration(probe.TimeoutSeconds)}
	res, err := client.Get(probeUrl.String())
	if err != nil {
		return false, err
	}

	return res.StatusCode == http.StatusOK, nil
}

func int32ToDuration(i int32) time.Duration {
	return time.Duration(int64(i))
}
