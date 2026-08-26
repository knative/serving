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
	"testing"

	netapi "knative.dev/networking/pkg/apis/networking"
	netcfg "knative.dev/networking/pkg/config"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/serving/pkg/reconciler/domainmapping/config"
)

func TestServingReferenceKind(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ref      duckv1.KReference
		wantKind string
		wantOK   bool
	}{{
		name:     "Knative Service",
		ref:      duckv1.KReference{APIVersion: "serving.knative.dev/v1", Kind: "Service"},
		wantKind: "Service",
		wantOK:   true,
	}, {
		name:     "Knative Route",
		ref:      duckv1.KReference{APIVersion: "serving.knative.dev/v1", Kind: "Route"},
		wantKind: "Route",
		wantOK:   true,
	}, {
		name:     "Knative Service given by group",
		ref:      duckv1.KReference{Group: "serving.knative.dev", Kind: "Service"},
		wantKind: "Service",
		wantOK:   true,
	}, {
		name: "core Service is not a Knative target",
		ref:  duckv1.KReference{APIVersion: "v1", Kind: "Service"},
	}, {
		name: "other Knative kind",
		ref:  duckv1.KReference{APIVersion: "serving.knative.dev/v1", Kind: "Configuration"},
	}, {
		name: "unparsable apiVersion",
		ref:  duckv1.KReference{APIVersion: "a/b/c", Kind: "Service"},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			gotKind, gotOK := servingReferenceKind(tc.ref)
			if gotKind != tc.wantKind || gotOK != tc.wantOK {
				t.Errorf("servingReferenceKind() = (%q, %t), want (%q, %t)", gotKind, gotOK, tc.wantKind, tc.wantOK)
			}
		})
	}
}

func TestExternalDomainTLSEnabled(t *testing.T) {
	dm := domainMapping("test-ns", "test-route")

	for _, tc := range []struct {
		name                           string
		configExternalDomainTLSEnabled bool
		tlsDisabledAnnotation          string
		wantExternalDomainTLSEnabled   bool
	}{{
		name:                           "ExternalDomainTLS enabled by config, not disabled by annotation",
		configExternalDomainTLSEnabled: true,
		wantExternalDomainTLSEnabled:   true,
	}, {
		name:                           "ExternalDomainTLS enabled by config, disabled by annotation",
		configExternalDomainTLSEnabled: true,
		tlsDisabledAnnotation:          "true",
		wantExternalDomainTLSEnabled:   false,
	}, {
		name:                           "ExternalDomainTLS disabled by config, not disabled by annotation",
		configExternalDomainTLSEnabled: false,
		wantExternalDomainTLSEnabled:   false,
	}, {
		name:                           "ExternalDomainTLS disabled by config, disabled by annotation",
		configExternalDomainTLSEnabled: false,
		tlsDisabledAnnotation:          "true",
		wantExternalDomainTLSEnabled:   false,
	}, {
		name:                           "ExternalDomainTLS enabled by config, invalid annotation",
		configExternalDomainTLSEnabled: true,
		tlsDisabledAnnotation:          "foo",
		wantExternalDomainTLSEnabled:   true,
	}, {
		name:                           "ExternalDomainTLS disabled by config, invalid annotation",
		configExternalDomainTLSEnabled: false,
		tlsDisabledAnnotation:          "foo",
		wantExternalDomainTLSEnabled:   false,
	}, {
		name:                           "ExternalDomainTLS disabled by config nil annotations",
		configExternalDomainTLSEnabled: false,
		wantExternalDomainTLSEnabled:   false,
	}, {
		name:                           "ExternalDomainTLS enabled by config, nil annotations",
		configExternalDomainTLSEnabled: true,
		wantExternalDomainTLSEnabled:   true,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := logtesting.TestContextWithLogger(t)
			ctx = config.ToContext(ctx, &config.Config{
				Network: &netcfg.Config{
					ExternalDomainTLS: tc.configExternalDomainTLSEnabled,
				},
			})
			if tc.tlsDisabledAnnotation != "" {
				dm.Annotations = map[string]string{
					netapi.DisableExternalDomainTLSAnnotationKey: tc.tlsDisabledAnnotation,
				}
			}
			if got := externalDomainTLSEnabled(ctx, dm); got != tc.wantExternalDomainTLSEnabled {
				t.Errorf("externalDomainTLSEnabled = %t, want %t", got, tc.wantExternalDomainTLSEnabled)
			}
		})
	}
}
