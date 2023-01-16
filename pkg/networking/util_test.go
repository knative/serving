package networking

import (
	"errors"
	"fmt"
	"testing"

	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	netcfg "knative.dev/networking/pkg/config"
	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/serving/pkg/reconciler/domainmapping/config"
)

func TestGetHTTPOption(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		configHTTPProtocol     netcfg.HTTPProtocol
		annotationHTTPProtocol netcfg.HTTPProtocol
		wantHTTPOption         netv1alpha1.HTTPOption
		wantError              error
	}{{
		name:                   "HTTPProtocol enabled by config, enabled by annotation",
		configHTTPProtocol:     netcfg.HTTPEnabled,
		annotationHTTPProtocol: netcfg.HTTPEnabled,
		wantHTTPOption:         netv1alpha1.HTTPOptionEnabled,
	}, {
		name:                   "HTTPProtocol enabled by config, redirected by annotation",
		configHTTPProtocol:     netcfg.HTTPEnabled,
		annotationHTTPProtocol: netcfg.HTTPRedirected,
		wantHTTPOption:         netv1alpha1.HTTPOptionRedirected,
	}, {
		name:                   "HTTPProtocol enabled by config, invalid by annotation",
		configHTTPProtocol:     netcfg.HTTPEnabled,
		annotationHTTPProtocol: "foo",
		wantError:              errors.New("incorrect http-protocol annotation: foo"),
	}, {
		name:                   "HTTPProtocol redirected by config, enabled by annotation",
		configHTTPProtocol:     netcfg.HTTPRedirected,
		annotationHTTPProtocol: netcfg.HTTPEnabled,
		wantHTTPOption:         netv1alpha1.HTTPOptionEnabled,
	}, {
		name:                   "HTTPProtocol redirected by config, redirected by annotation",
		configHTTPProtocol:     netcfg.HTTPRedirected,
		annotationHTTPProtocol: netcfg.HTTPRedirected,
		wantHTTPOption:         netv1alpha1.HTTPOptionRedirected,
	}, {
		name:                   "HTTPProtocol redirected by config, invalid by annotation",
		configHTTPProtocol:     netcfg.HTTPRedirected,
		annotationHTTPProtocol: "foo",
		wantError:              errors.New("incorrect http-protocol annotation: foo"),
	}, {
		name:               "HTTPProtocol enabled by config, nil annotations",
		configHTTPProtocol: netcfg.HTTPEnabled,
		wantHTTPOption:     netv1alpha1.HTTPOptionEnabled,
	}, {
		name:               "HTTPProtocol redirected by config, nil annotations",
		configHTTPProtocol: netcfg.HTTPRedirected,
		wantHTTPOption:     netv1alpha1.HTTPOptionRedirected,
	}, {
		name:               "HTTPProtocol disabled by config, nil annotations",
		configHTTPProtocol: netcfg.HTTPDisabled,
		wantHTTPOption:     "",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := logtesting.TestContextWithLogger(t)
			ctx = config.ToContext(ctx, &config.Config{
				Network: &netcfg.Config{
					HTTPProtocol: tc.configHTTPProtocol,
				},
			})

			var annotations map[string]string
			if tc.annotationHTTPProtocol != "" {
				annotations = map[string]string{
					networking.HTTPProtocolAnnotationKey: string(tc.annotationHTTPProtocol),
				}
			}

			got, err := GetHTTPOption(ctx, &netcfg.Config{HTTPProtocol: tc.configHTTPProtocol}, annotations)
			if tc.wantError != nil && fmt.Sprintf("%s", err) != fmt.Sprintf("%s", tc.wantError) {
				t.Errorf("err = %s, want %v", err, tc.wantError)
			}
			if tc.wantError == nil && got != tc.wantHTTPOption {
				t.Errorf("GetHTTPOption = %s, want %s", got, tc.wantHTTPOption)
			}
		})
	}
}
