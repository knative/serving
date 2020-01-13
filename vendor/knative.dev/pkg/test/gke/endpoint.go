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

package gke

import (
	"fmt"
	"regexp"
)

const (
	testEnv     = "test"
	stagingEnv  = "staging"
	staging2Env = "staging2"
	prodEnv     = "prod"

	testEndpoint     = "https://test-container.sandbox.googleapis.com/"
	stagingEndpoint  = "https://staging-container.sandbox.googleapis.com/"
	staging2Endpoint = "https://staging2-container.sandbox.googleapis.com/"
	prodEndpoint     = "https://container.googleapis.com/"
)

var urlRe = regexp.MustCompile(`https://.*/`)

// ServiceEndpoint returns the container service endpoint for the given environment.
func ServiceEndpoint(environment string) (string, error) {
	var endpoint string
	switch env := environment; {
	case env == testEnv:
		endpoint = testEndpoint
	case env == stagingEnv:
		endpoint = stagingEndpoint
	case env == staging2Env:
		endpoint = staging2Endpoint
	case env == prodEnv:
		endpoint = prodEndpoint
	case urlRe.MatchString(env):
		endpoint = env
	default:
		return "", fmt.Errorf("the environment '%s' is invalid, must be one of 'test', 'staging', 'staging2', 'prod', or a custom https:// URL", environment)
	}
	return endpoint, nil
}
