/*
Copyright 2024 The Knative Authors

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

package drain

import (
	"strings"
	"testing"
)

func TestConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{
			name:     "SignalDirectory",
			got:      SignalDirectory,
			expected: "/var/run/knative",
		},
		{
			name:     "DrainStartedFile",
			got:      DrainStartedFile,
			expected: "/var/run/knative/drain-started",
		},
		{
			name:     "DrainCompleteFile",
			got:      DrainCompleteFile,
			expected: "/var/run/knative/drain-complete",
		},
		{
			name:     "DrainCheckInterval",
			got:      DrainCheckInterval,
			expected: "1",
		},
		{
			name:     "ExponentialBackoffDelays",
			got:      ExponentialBackoffDelays,
			expected: "1 2 4 8",
		},
		{
			name:     "QueueProxyPreStopScript",
			got:      QueueProxyPreStopScript,
			expected: "touch /var/run/knative/drain-started",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, want %q", tt.got, tt.expected)
			}
		})
	}
}

func TestBuildDrainWaitScript(t *testing.T) {
	tests := []struct {
		name            string
		existingCommand string
		wantContains    []string
		wantExact       bool
	}{
		{
			name:            "without existing command",
			existingCommand: "",
			wantContains: []string{
				"for i in 1 2 4 8",
				"if [ -f /var/run/knative/drain-started ]",
				"until [ -f /var/run/knative/drain-complete ]",
				"sleep 1",
				"sleep $i",
				"exit 0",
			},
			wantExact: false,
		},
		{
			name:            "with existing command",
			existingCommand: "echo 'custom prestop'",
			wantContains: []string{
				"echo 'custom prestop'",
				"for i in 1 2 4 8",
				"if [ -f /var/run/knative/drain-started ]",
				"until [ -f /var/run/knative/drain-complete ]",
				"sleep 1",
				"sleep $i",
				"exit 0",
			},
			wantExact: false,
		},
		{
			name:            "with complex existing command",
			existingCommand: "/bin/sh -c 'kill -TERM 1 && wait'",
			wantContains: []string{
				"/bin/sh -c 'kill -TERM 1 && wait'",
				"for i in 1 2 4 8",
				"if [ -f /var/run/knative/drain-started ]",
				"until [ -f /var/run/knative/drain-complete ]",
			},
			wantExact: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildDrainWaitScript(tt.existingCommand)

			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("BuildDrainWaitScript() missing expected substring %q\nGot: %q", want, got)
				}
			}

			// Verify the command structure
			if tt.existingCommand != "" {
				// Should start with the existing command
				if !strings.HasPrefix(got, tt.existingCommand+"; ") {
					t.Errorf("BuildDrainWaitScript() should start with existing command followed by '; '\nGot: %q", got)
				}
			}

			// Verify the script ends with exit 0
			if !strings.HasSuffix(got, "exit 0") {
				t.Errorf("BuildDrainWaitScript() should end with 'exit 0'\nGot: %q", got)
			}
		})
	}
}

func TestBuildDrainWaitScriptStructure(t *testing.T) {
	// Test the exact structure of the generated script without existing command
	got := BuildDrainWaitScript("")
	expected := "for i in 1 2 4 8; do " +
		"  if [ -f /var/run/knative/drain-started ]; then " +
		"    until [ -f /var/run/knative/drain-complete ]; do sleep 1; done; " +
		"    exit 0; " +
		"  fi; " +
		"  sleep $i; " +
		"done; " +
		"exit 0"

	if got != expected {
		t.Errorf("BuildDrainWaitScript(\"\") structure mismatch\nGot:      %q\nExpected: %q", got, expected)
	}
}

func TestBuildDrainWaitScriptWithCommandStructure(t *testing.T) {
	// Test the exact structure of the generated script with existing command
	existingCmd := "echo 'test'"
	got := BuildDrainWaitScript(existingCmd)
	expected := "echo 'test'; for i in 1 2 4 8; do " +
		"  if [ -f /var/run/knative/drain-started ]; then " +
		"    until [ -f /var/run/knative/drain-complete ]; do sleep 1; done; " +
		"    exit 0; " +
		"  fi; " +
		"  sleep $i; " +
		"done; " +
		"exit 0"

	if got != expected {
		t.Errorf("BuildDrainWaitScript with command structure mismatch\nGot:      %q\nExpected: %q", got, expected)
	}
}

func TestBuildDrainWaitScriptEdgeCases(t *testing.T) {
	tests := []struct {
		name            string
		existingCommand string
		checkFunc       func(t *testing.T, result string)
	}{
		{
			name:            "empty string produces valid script",
			existingCommand: "",
			checkFunc: func(t *testing.T, result string) {
				if result == "" {
					t.Error("BuildDrainWaitScript(\"\") should not return empty string")
				}
				if !strings.Contains(result, "for i in") {
					t.Error("BuildDrainWaitScript(\"\") should contain for loop")
				}
			},
		},
		{
			name:            "command with semicolon",
			existingCommand: "cmd1; cmd2",
			checkFunc: func(t *testing.T, result string) {
				if !strings.HasPrefix(result, "cmd1; cmd2; ") {
					t.Error("BuildDrainWaitScript should preserve command with semicolons")
				}
			},
		},
		{
			name:            "command with special characters",
			existingCommand: "echo '$VAR' && test -f /tmp/file",
			checkFunc: func(t *testing.T, result string) {
				if !strings.HasPrefix(result, "echo '$VAR' && test -f /tmp/file; ") {
					t.Error("BuildDrainWaitScript should preserve special characters")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildDrainWaitScript(tt.existingCommand)
			tt.checkFunc(t, result)
		})
	}
}

func BenchmarkBuildDrainWaitScript(b *testing.B) {
	testCases := []struct {
		name    string
		command string
	}{
		{"NoCommand", ""},
		{"SimpleCommand", "echo test"},
		{"ComplexCommand", "/bin/sh -c 'kill -TERM 1 && wait'"},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			for range b.N {
				_ = BuildDrainWaitScript(tc.command)
			}
		})
	}
}
