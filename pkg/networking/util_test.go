package networking

import (
	"errors"
	"fmt"
	"testing"

	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/serving/pkg/reconciler/domainmapping/config"
)

func TestGetHTTPOption(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		configHTTPProtocol     network.HTTPProtocol
		annotationHTTPProtocol network.HTTPProtocol
		wantHTTPOption         netv1alpha1.HTTPOption
		wantError              error
	}{{
		name:                   "HTTPProtocol enabled by config, enabled by annotation",
		configHTTPProtocol:     network.HTTPEnabled,
		annotationHTTPProtocol: network.HTTPEnabled,
		wantHTTPOption:         netv1alpha1.HTTPOptionEnabled,
	}, {
		name:                   "HTTPProtocol enabled by config, redirected by annotation",
		configHTTPProtocol:     network.HTTPEnabled,
		annotationHTTPProtocol: network.HTTPRedirected,
		wantHTTPOption:         netv1alpha1.HTTPOptionRedirected,
	}, {
		name:                   "HTTPProtocol enabled by config, invalid by annotation",
		configHTTPProtocol:     network.HTTPEnabled,
		annotationHTTPProtocol: "foo",
		wantError:              errors.New("incorrect http-protocol annotation: foo"),
	}, {
		name:                   "HTTPProtocol redirected by config, enabled by annotation",
		configHTTPProtocol:     network.HTTPRedirected,
		annotationHTTPProtocol: network.HTTPEnabled,
		wantHTTPOption:         netv1alpha1.HTTPOptionEnabled,
	}, {
		name:                   "HTTPProtocol redirected by config, redirected by annotation",
		configHTTPProtocol:     network.HTTPRedirected,
		annotationHTTPProtocol: network.HTTPRedirected,
		wantHTTPOption:         netv1alpha1.HTTPOptionRedirected,
	}, {
		name:                   "HTTPProtocol redirected by config, invalid by annotation",
		configHTTPProtocol:     network.HTTPRedirected,
		annotationHTTPProtocol: "foo",
		wantError:              errors.New("incorrect http-protocol annotation: foo"),
	}, {
		name:               "HTTPProtocol enabled by config, nil annotations",
		configHTTPProtocol: network.HTTPEnabled,
		wantHTTPOption:     netv1alpha1.HTTPOptionEnabled,
	}, {
		name:               "HTTPProtocol redirected by config, nil annotations",
		configHTTPProtocol: network.HTTPRedirected,
		wantHTTPOption:     netv1alpha1.HTTPOptionRedirected,
	}, {
		name:               "HTTPProtocol disabled by config, nil annotations",
		configHTTPProtocol: network.HTTPDisabled,
		wantHTTPOption:     "",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := logtesting.TestContextWithLogger(t)
			ctx = config.ToContext(ctx, &config.Config{
				Network: &network.Config{
					HTTPProtocol: tc.configHTTPProtocol,
				},
			})

			var annotations map[string]string
			if tc.annotationHTTPProtocol != "" {
				annotations = map[string]string{
					networking.HTTPProtocolAnnotationKey: string(tc.annotationHTTPProtocol),
				}
			}

			got, err := GetHTTPOption(ctx, &network.Config{HTTPProtocol: tc.configHTTPProtocol}, annotations)
			if tc.wantError != nil && fmt.Sprintf("%s", err) != fmt.Sprintf("%s", tc.wantError) {
				t.Errorf("err = %s, want %v", err, tc.wantError)
			}
			if tc.wantError == nil && got != tc.wantHTTPOption {
				t.Errorf("GetHTTPOption = %s, want %s", got, tc.wantHTTPOption)
			}
		})
	}
}
