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
	"errors"
	"fmt"
	"testing"

	network "knative.dev/networking/pkg"
	networkingpkg "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	logtesting "knative.dev/pkg/logging/testing"
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
			ctx := logtesting.TestContextWithLogger(t)
			ctx = config.ToContext(ctx, &config.Config{
				Network: &network.Config{
					AutoTLS: tc.configAutoTLSEnabled,
				},
			})
			if tc.tlsDisabledAnnotation != "" {
				dm.Annotations = map[string]string{
					networking.DisableAutoTLSAnnotationKey: tc.tlsDisabledAnnotation,
				}
			}
			if got := autoTLSEnabled(ctx, dm); got != tc.wantAutoTLSEnabled {
				t.Errorf("autoTLSEnabled = %t, want %t", got, tc.wantAutoTLSEnabled)
			}
		})
	}
}

func TestHTTPOption(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		configHTTPProtocol     networkingpkg.HTTPProtocol
		annotationHTTPProtocol networkingpkg.HTTPProtocol
		wantHTTPOption         netv1alpha1.HTTPOption
		wantError              error
	}{{
		name:                   "HTTPProtocol enabled by config, enabled by annotation",
		configHTTPProtocol:     networkingpkg.HTTPEnabled,
		annotationHTTPProtocol: networkingpkg.HTTPEnabled,
		wantHTTPOption:         netv1alpha1.HTTPOptionEnabled,
	}, {
		name:                   "HTTPProtocol enabled by config, redirected by annotation",
		configHTTPProtocol:     networkingpkg.HTTPEnabled,
		annotationHTTPProtocol: networkingpkg.HTTPRedirected,
		wantHTTPOption:         netv1alpha1.HTTPOptionRedirected,
	}, {
		name:                   "HTTPProtocol enabled by config, invalid by annotation",
		configHTTPProtocol:     networkingpkg.HTTPEnabled,
		annotationHTTPProtocol: "foo",
		wantError:              errors.New("incorrect http-protocol annotation: foo"),
	}, {
		name:                   "HTTPProtocol redirected by config, enabled by annotation",
		configHTTPProtocol:     networkingpkg.HTTPRedirected,
		annotationHTTPProtocol: networkingpkg.HTTPEnabled,
		wantHTTPOption:         netv1alpha1.HTTPOptionEnabled,
	}, {
		name:                   "HTTPProtocol redirected by config, redirected by annotation",
		configHTTPProtocol:     networkingpkg.HTTPRedirected,
		annotationHTTPProtocol: networkingpkg.HTTPRedirected,
		wantHTTPOption:         netv1alpha1.HTTPOptionRedirected,
	}, {
		name:                   "HTTPProtocol redirected by config, invalid by annotation",
		configHTTPProtocol:     networkingpkg.HTTPRedirected,
		annotationHTTPProtocol: "foo",
		wantError:              errors.New("incorrect http-protocol annotation: foo"),
	}, {
		name:               "HTTPProtocol enabled by config, nil annotations",
		configHTTPProtocol: networkingpkg.HTTPEnabled,
		wantHTTPOption:     netv1alpha1.HTTPOptionEnabled,
	}, {
		name:               "HTTPProtocol redirected by config, nil annotations",
		configHTTPProtocol: networkingpkg.HTTPRedirected,
		wantHTTPOption:     netv1alpha1.HTTPOptionRedirected,
	}, {
		name:               "HTTPProtocol disabled by config, nil annotations",
		configHTTPProtocol: networkingpkg.HTTPDisabled,
		wantHTTPOption:     "",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := logtesting.TestContextWithLogger(t)
			ctx = config.ToContext(ctx, &config.Config{
				Network: &network.Config{
					HTTPProtocol: tc.configHTTPProtocol,
				},
			})

			dm := domainMapping("test-ns", "test-route")
			if tc.annotationHTTPProtocol != "" {
				dm.Annotations = map[string]string{
					networking.HTTPProtocolAnnotationKey: string(tc.annotationHTTPProtocol),
				}
			}

			got, err := httpOption(ctx, dm.GetAnnotations())
			if tc.wantError != nil && fmt.Sprintf("%s", err) != fmt.Sprintf("%s", tc.wantError) {
				t.Errorf("err = %s, want %v", err, tc.wantError)
			}
			if tc.wantError == nil && got != tc.wantHTTPOption {
				t.Errorf("httpOption = %s, want %s", got, tc.wantHTTPOption)
			}
		})
	}
}
