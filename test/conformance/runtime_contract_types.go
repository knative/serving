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

package conformance

import (
	"strconv"

	"github.com/knative/serving/test"
)

//runtime_constract_types.go defines types that encapsulate run-time contract requirements as specified here: https://github.com/knative/serving/blob/master/docs/runtime-contract.md

// ShouldEnvVars defines environment variables that "SHOULD" be set.
// To match these values with test service parameters,
// map values must represent corresponding test.ResourceNames fields
var ShouldEnvVars = map[string]string{
	"K_SERVICE":       "Service",
	"K_CONFIGURATION": "Config",
	"K_REVISION":      "Revision",
}

// MustEnvVars defines environment variables that "MUST" be set.
var MustEnvVars = map[string]string{
	"PORT": strconv.Itoa(test.EnvImageServerPort),
}

// FilePathInfo data object returned by the environment test-image.
type FilePathInfo struct {
	FilePath    string
	IsDirectory bool
	PermString  string
}

// MustFilePathSpecs specifies the file-paths and expected permissions that MUST be set as specified in the runtime contract.
var MustFilePathSpecs = map[string]FilePathInfo{
	"/tmp": {
		IsDirectory: true,
		PermString:  "rw*rw*rw*", // * indicates no specification
	},
	"/var/log": {
		IsDirectory: true,
		PermString:  "rw*rw*rw*", // * indicates no specification
	},
	// TODO(#822): Add conformance tests for "/dev/log".
}

// ShouldFilePathSpecs specifies the file-paths and expected permissions that SHOULD be set as specified in the run-time contract.
var ShouldFilePathSpecs = map[string]FilePathInfo{
	"/etc/resolv.conf": {
		IsDirectory: false,
		PermString:  "rw*r**r**", // * indicates no specification
	},
}
