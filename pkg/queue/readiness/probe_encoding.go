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
	"encoding/json"
	"errors"

	"knative.dev/serving/pkg/queue/health"
)

// DecodeProbe takes a json serialised *corev1.Probe and returns a Probe or an error.
func DecodeProbe(jsonProbe string) (*health.Probe, error) {
	p := &health.Probe{}
	if err := json.Unmarshal([]byte(jsonProbe), p); err != nil {
		return nil, err
	}
	return p, nil
}

// EncodeProbe takes *corev1.Probe object and returns marshalled Probe JSON string and an error.
func EncodeProbe(rp *health.Probe) (string, error) {
	if rp == nil {
		return "", errors.New("cannot encode nil probe")
	}

	probeJSON, err := json.Marshal(rp)
	if err != nil {
		return "", err
	}
	return string(probeJSON), nil
}
