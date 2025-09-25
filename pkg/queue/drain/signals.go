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

const (
	// SignalDirectory is the directory where drain signal files are created
	SignalDirectory = "/var/run/knative"

	// DrainStartedFile indicates that pod termination has begun and queue-proxy is handling shutdown
	DrainStartedFile = SignalDirectory + "/drain-started"

	// DrainCompleteFile indicates that queue-proxy has finished draining requests
	DrainCompleteFile = SignalDirectory + "/drain-complete"

	// DrainCheckInterval is how often to check for drain completion
	DrainCheckInterval = "1" // seconds

	// ExponentialBackoffDelays are the delays in seconds for checking drain-started file
	// Total max wait time: 1+2+4+8 = 15 seconds
	ExponentialBackoffDelays = "1 2 4 8"
)

// BuildDrainWaitScript generates the shell script for waiting on drain signals.
// If existingCommand is provided, it will be executed before the drain wait.
func BuildDrainWaitScript(existingCommand string) string {
	drainLogic := `for i in ` + ExponentialBackoffDelays + `; do ` +
		`  if [ -f ` + DrainStartedFile + ` ]; then ` +
		`    until [ -f ` + DrainCompleteFile + ` ]; do sleep ` + DrainCheckInterval + `; done; ` +
		`    exit 0; ` +
		`  fi; ` +
		`  sleep $i; ` +
		`done; ` +
		`exit 0`

	if existingCommand != "" {
		return existingCommand + "; " + drainLogic
	}
	return drainLogic
}

// QueueProxyPreStopScript is the script executed by queue-proxy's PreStop hook
const QueueProxyPreStopScript = "touch " + DrainStartedFile
