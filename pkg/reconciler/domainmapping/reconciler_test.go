/*
Copyright 2021 The Knative Authors

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

package domainmapping

import (
	"context"
	"testing"

	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/serving/pkg/reconciler/domainmapping/config"
)

func TestAutoTLSEnabled(t *testing.T) {
	dm := domainMapping("test-ns", "test-route")

	for _, tc := range []struct {
		name                  string
		configAutoTLSEnabled  bool
		tlsDisabledAnnotation string
		wantAutoTLSEnabled    bool
	}{{
		name:                 "AutoTLS enabled by config, not disabled by annotation",
		configAutoTLSEnabled: true,
		wantAutoTLSEnabled:   true,
	}, {
		name:                  "AutoTLS enabled by config, disabled by annotation",
		configAutoTLSEnabled:  true,
		tlsDisabledAnnotation: "true",
		wantAutoTLSEnabled:    false,
	}, {
		name:                 "AutoTLS disabled by config, not disabled by annotation",
		configAutoTLSEnabled: false,
		wantAutoTLSEnabled:   false,
	}, {
		name:                  "AutoTLS disabled by config, disabled by annotation",
		configAutoTLSEnabled:  false,
		tlsDisabledAnnotation: "true",
		wantAutoTLSEnabled:    false,
	}, {
		name:                  "AutoTLS enabled by config, invalid annotation",
		configAutoTLSEnabled:  true,
		tlsDisabledAnnotation: "foo",
		wantAutoTLSEnabled:    true,
	}, {
		name:                  "AutoTLS disabled by config, invalid annotation",
		configAutoTLSEnabled:  false,
		tlsDisabledAnnotation: "foo",
		wantAutoTLSEnabled:    false,
	}, {
		name:                 "AutoTLS disabled by config nil annotations",
		configAutoTLSEnabled: false,
		wantAutoTLSEnabled:   false,
	}, {
		name:                 "AutoTLS enabled by config, nil annotations",
		configAutoTLSEnabled: true,
		wantAutoTLSEnabled:   true,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := config.ToContext(context.Background(), &config.Config{
				Network: &network.Config{
					AutoTLS: tc.configAutoTLSEnabled,
				},
			})
			if tc.tlsDisabledAnnotation != "" {
				dm.Annotations = map[string]string{}
				dm.Annotations[networking.DisableAutoTLSAnnotationKey] = tc.tlsDisabledAnnotation
			}
			if got := autoTLSEnabled(ctx, dm); got != tc.wantAutoTLSEnabled {
				t.Errorf("autoTLSEnabled = %t, want %t", got, tc.wantAutoTLSEnabled)
			}
		})
	}
}
