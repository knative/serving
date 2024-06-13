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

package main

import "testing"

func TestBuildMagicDNSName(t *testing.T) {
	for _, tt := range []struct {
		name     string
		magicDNS string
		ip       string
		want     string
	}{
		{
			name:     "ipv4",
			magicDNS: "sslip.io",
			ip:       "1.1.1.1",
			want:     "1.1.1.1.sslip.io",
		},
		{
			name:     "ipv6",
			magicDNS: "sslip.io",
			ip:       "2a01:4f8:c17:b8f::2",
			want:     "2a01-4f8-c17-b8f--2.sslip.io",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildMagicDNSName(tt.ip, tt.magicDNS); got != tt.want {
				t.Errorf("buildMagicDNSName() = %v, want %v", got, tt.want)
			}
		})
	}
}
