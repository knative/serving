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

package test

// crd contains functions that construct boilerplate CRD definitions.

import (
	"strings"
	"testing"

	"github.com/knative/pkg/test/helpers"
)

// ResourceNames holds names of various resources.
type ResourceNames struct {
	Config        string
	Route         string
	Revision      string
	Service       string
	TrafficTarget string
	Domain        string
	Image         string
}

// AppendRandomString will generate a random string that begins with prefix. This is useful
// if you want to make sure that your tests can run at the same time against the same
// environment without conflicting. This method will seed rand with the current time when
// called for the first time.
var AppendRandomString = helpers.AppendRandomString

// MakeK8sNamePrefix will convert each chunk of non-alphanumeric character into a single dash
// and also convert camelcase tokens into dash-delimited lowercase tokens.
var MakeK8sNamePrefix = helpers.MakeK8sNamePrefix

// ObjectNameForTest generates a random object name based on the test name.
var ObjectNameForTest = helpers.ObjectNameForTest

// SubServiceNameForTest generates a random service name based on the test name and
// the given subservice name.
func SubServiceNameForTest(t *testing.T, subsvc string) string {
	fullPrefix := strings.TrimPrefix(t.Name(), "Test") + "-" + subsvc
	return AppendRandomString(MakeK8sNamePrefix(fullPrefix))
}
