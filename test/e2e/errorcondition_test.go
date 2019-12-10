// +build e2e

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

package e2e

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/test/logging"
	"knative.dev/serving/test"
	"knative.dev/serving/test/scenarios"
	v1 "knative.dev/serving/test/v1"
)

func TestMustErrorOnContainerError(legacy *testing.T) {
	t, cancel := logging.NewTLogger(legacy)
	defer cancel()

	manifestUnknown := string(transport.ManifestUnknownErrorCode)

	scenarios.ContainerError(t,
		func(ts *logging.TLogger, cond *apis.Condition) (bool, error) {
			// API Spec does not have constraints on the Message,
			// but we want to confirm it failed for the correct reason
			if strings.Contains(cond.Message, manifestUnknown) {
				return true, nil
			}
			ts.Fatal("The configuration was not marked with expected error condition",
				"wantMessage", manifestUnknown, "wantStatus", "False")
			return true, errors.New("Shouldn't get here")
		},
		func(ts *logging.TLogger, cond *apis.Condition) (bool, error) {
			// API Spec does not have constraints on the Message,
			// but we want to confirm it failed for the correct reason
			if cond.Reason == v1.ContainerMissing && strings.Contains(cond.Message, manifestUnknown) {
				return true, nil
			}
			ts.Fatal("The revision was not marked with expected error condition",
				"wantReason", v1.ContainerMissing, "wantMessage", "!\"\"")
			return true, errors.New("Shouldn't get here")
		})
}

func TestMustErrorOnContainerExiting(legacy *testing.T) {
	t, cancel := logging.NewTLogger(legacy)
	defer cancel()
	scenarios.ContainerExiting(t,
		func(ts *logging.TLogger, cond *apis.Condition) (bool, error) {
			// API Spec does not have constraints on the Message,
			// but we want to confirm it failed for the correct reason
			if strings.Contains(cond.Message, test.ErrorLog) {
				return true, nil
			}
			ts.Fatal("The configuration was not marked with expected error condition.",
				"wantMessage", test.ErrorLog, "wantStatus", "False")
			return true, errors.New("Shouldn't get here")
		},
		func(ts *logging.TLogger, cond *apis.Condition) (bool, error) {
			// API Spec does not have constraints on the Message,
			// but we want to confirm it failed for the correct reason
			if cond.Reason == test.ExitCodeReason && strings.Contains(cond.Message, test.ErrorLog) {
				return true, nil
			}
			ts.Fatal("The revision was not marked with expected error condition.",
				"wantReason", test.ExitCodeReason, "wantMessage", test.ErrorLog)
			return true, errors.New("Shouldn't get here")
		})
}
