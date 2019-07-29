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
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestParseProbeSuccess(t *testing.T) {
	expectedProbe := &corev1.Probe{
		PeriodSeconds:    1,
		TimeoutSeconds:   2,
		SuccessThreshold: 1,
		FailureThreshold: 1,
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: "127.0.0.1",
				Port: intstr.FromString("8080"),
			},
		},
	}
	probeBytes, err := json.Marshal(expectedProbe)
	if err != nil {
		t.Fatalf("Failed to decode probe %#v", err)
	}
	gotProbe, err := DecodeProbe(string(probeBytes))
	if err != nil {
		t.Fatalf("Failed DecodeProbe() %#v", err)
	}
	if d := cmp.Diff(gotProbe, expectedProbe); d != "" {
		t.Errorf("Probe diff %s; got %v, want %v", d, gotProbe, expectedProbe)
	}
}

func TestParseProbeFailure(t *testing.T) {
	probeBytes, err := json.Marshal("wrongProbeObject")
	if err != nil {
		t.Fatalf("Failed to decode probe %#v", err)
	}
	_, err = DecodeProbe(string(probeBytes))
	if err == nil {
		t.Fatal("Expected DecodeProbe() to fail")
	}
}

func TestEncodeProbe(t *testing.T) {
	probe := &corev1.Probe{
		SuccessThreshold: 1,
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Host: "127.0.0.1",
				Port: intstr.FromString("8080"),
			},
		},
	}

	jsonProbe, err := EncodeProbe(probe)

	if err != nil {
		t.Fatalf("Expected no errer, got: %#v", err)
	}

	wantProbe := `{"tcpSocket":{"port":"8080","host":"127.0.0.1"},"successThreshold":1}`

	if diff := cmp.Diff(jsonProbe, wantProbe); diff != "" {
		t.Errorf("Probe diff: %s; got %v, want %v", diff, jsonProbe, wantProbe)
	}
}

func TestEncodeNilProbe(t *testing.T) {
	jsonProbe, err := EncodeProbe(nil)

	if err == nil {
		t.Errorf("Expected error")
	}

	if jsonProbe != "" {
		t.Errorf("Expected empty probe string; got %s", jsonProbe)
	}
}
