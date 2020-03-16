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

// env.go provides a central point to read all environment variables defined by Prow.

package prow

import (
	"errors"
	"fmt"

	"github.com/kelseyhightower/envconfig"
)

// EnvConfig consists of all the environment variables that can be set in a Prow job,
// check https://github.com/kubernetes/test-infra/blob/master/prow/jobs.md#job-environment-variables
// for more information.
type EnvConfig struct {
	CI          bool
	Artifacts   string
	JobName     string `split_words:"true"`
	JobType     string `split_words:"true"`
	JobSpec     string `split_words:"true"`
	BuildID     string `envconfig:"BUILD_ID"`
	ProwJobID   string `envconfig:"PROW_JOB_ID"`
	RepoOwner   string `split_words:"true"`
	RepoName    string `split_words:"true"`
	PullBaseRef string `split_words:"true"`
	PullBaseSha string `split_words:"true"`
	PullRefs    string `split_words:"true"`
	PullNumber  uint   `split_words:"true"`
	PullPullSha string `split_words:"true"`
}

// GetEnvConfig returns values of all the environment variables that can be possibly set in a Prow job.
func GetEnvConfig() (*EnvConfig, error) {
	var ec EnvConfig
	if err := envconfig.Process("", &ec); err != nil {
		return nil, fmt.Errorf("failed getting environment variables for Prow: %w", err)
	}

	if !ec.CI {
		return nil, errors.New("this function is not expected to be called from a non-CI environment")
	}
	return &ec, nil
}
