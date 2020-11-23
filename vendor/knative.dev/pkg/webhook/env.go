/*
Copyright 2020 The Knative Authors

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

package webhook

import (
	"fmt"
	"os"
	"strconv"
)

const portEnvKey = "WEBHOOK_PORT"

// Webhook is the name of the override key used inside of the logging config for Webhook Controller.
const webhookNameEnv = "WEBHOOK_NAME"

// PortFromEnv returns the webhook port set by portEnvKey, or default port if env var is not set.
func PortFromEnv(defaultPort int) int {
	if os.Getenv(portEnvKey) == "" {
		return defaultPort
	}
	port, err := strconv.Atoi(os.Getenv(portEnvKey))
	if err != nil {
		panic(fmt.Sprintf("failed to convert the environment variable %q : %v", portEnvKey, err))
	} else if port == 0 {
		panic(fmt.Sprintf("the environment variable %q can't be zero", portEnvKey))
	}
	return port
}

func NameFromEnv() string {
	if webhook := os.Getenv(webhookNameEnv); webhook != "" {
		return webhook
	}

	panic(fmt.Sprintf(`The environment variable %q is not set.
This should be unique for the webhooks in a namespace
If this is a process running on Kubernetes, then initialize this variable via:
  env:
  - name: WEBHOOK_NAME
    value: webhook
`, webhookNameEnv))
}
