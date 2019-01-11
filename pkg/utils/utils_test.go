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

package utils

import (
	"strings"
	"testing"
)

func TestGetDomainName(t *testing.T) {
	tests := []struct {
		descr      string
		resolvConf string
		domainName string
		shouldFail bool
	}{
		{
			descr: "all good",
			resolvConf: `
nameserver 1.1.1.1
search default.svc.abc.com svc.abc.com abc.com
options ndots:5
`,
			domainName: "abc.com",
			shouldFail: false,
		},
		{
			descr: "missing search line",
			resolvConf: `
nameserver 1.1.1.1
options ndots:5
`,
			domainName: "",
			shouldFail: true,
		},
		{
			descr: "non k8s resolv.conf format",
			resolvConf: `
nameserver 1.1.1.1
search  abc.com xyz.com
options ndots:5
`,
			domainName: "",
			shouldFail: true,
		},
	}
	for _, tt := range tests {
		dn, err := getClusterDomainName(strings.NewReader(tt.resolvConf))
		if err != nil {
			if !tt.shouldFail {
				t.Errorf("Test %s failed with error: %q but is not supposed to.", tt.descr, err)
			} else {
				continue
			}
		}
		if err == nil {
			if tt.shouldFail {
				t.Errorf("Test %s succeeded but supposed to fail.", tt.descr)
				continue
			}
			if dn != tt.domainName {
				t.Errorf("Test %s failed expected %s but got %s", tt.descr, tt.domainName, dn)
			}
		}
	}
}
