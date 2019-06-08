/*
Copyright 2019 The Knative Authors

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
package domains

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/apis"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/gc"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler/route/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func testConfig() *config.Config {
	return &config.Config{
		Domain: &config.Domain{
			Domains: map[string]*config.LabelSelector{
				"example.com": {},
				"another-example.com": {
					Selector: map[string]string{"app": "prod"},
				},
			},
		},
		Network: &network.Config{
			DefaultClusterIngressClass: "ingress-class-foo",
			DomainTemplate:             network.DefaultDomainTemplate,
		},
		GC: &gc.Config{
			StaleRevisionLastpinnedDebounce: time.Duration(1 * time.Minute),
		},
	}
}

func TestDomainNameFromTemplate(t *testing.T) {
	type args struct {
		name string
	}
	tests := []struct {
		name     string
		template string
		args     args
		want     string
		wantErr  bool
	}{{
		name:     "Default",
		template: "{{.Name}}.{{.Namespace}}.{{.Domain}}",
		args:     args{name: "test-name"},
		want:     "test-name.default.example.com",
	}, {
		name:     "Dash",
		template: "{{.Name}}-{{.Namespace}}.{{.Domain}}",
		args:     args{name: "test-name"},
		want:     "test-name-default.example.com",
	}, {
		name:     "Short",
		template: "{{.Name}}.{{.Domain}}",
		args:     args{name: "test-name"},
		want:     "test-name.example.com",
	}, {
		name:     "SuperShort",
		template: "{{.Name}}",
		args:     args{name: "test-name"},
		want:     "test-name",
	}, {
		name:     "Annotations",
		template: `{{.Name}}.{{ index .Annotations "sub"}}.{{.Domain}}`,
		args:     args{name: "test-name"},
		want:     "test-name.mysub.example.com",
	}, {
		// This cannot get through our validation, but verify we handle errors.
		name:     "BadVarName",
		template: "{{.Name}}.{{.NNNamespace}}.{{.Domain}}",
		args:     args{name: "test-name"},
		wantErr:  true,
	}}

	route := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/serving/v1alpha1/namespaces/test/Routes/myapp",
			Name:      "myroute",
			Namespace: "default",
			Labels: map[string]string{
				"route": "myapp",
			},
			Annotations: map[string]string{
				"sub": "mysub",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := testConfig()
			cfg.Network.DomainTemplate = tt.template
			ctx = config.ToContext(ctx, cfg)

			got, err := DomainNameFromTemplate(ctx, route, tt.args.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("DomainNameFromTemplate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("DomainNameFromTemplate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestURL(t *testing.T) {
	tests := []struct {
		name     string
		scheme   string
		domain   string
		Expected apis.URL
	}{{
		name:   "subdomain",
		scheme: HTTPScheme,
		domain: "current.svc.local.com",
		Expected: apis.URL{
			Scheme: "http",
			Host:   "current.svc.local.com",
		},
	}, {
		name:   "default target",
		scheme: HTTPScheme,
		domain: "example.com",
		Expected: apis.URL{
			Scheme: "http",
			Host:   "example.com",
		},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, want := *URL(tt.scheme, tt.domain), tt.Expected; !cmp.Equal(want, got) {
				t.Errorf("URL = %v, want: %v", got, want)
			}
		})
	}
}

func TestGetAllDomainsAndTags(t *testing.T) {
	tests := []struct {
		name           string
		domainTemplate string
		tagTemplate    string
		want           map[string]string
		wantErr        bool
	}{{
		name:           "happy case",
		domainTemplate: "{{.Name}}.{{.Namespace}}.{{.Domain}}",
		tagTemplate:    "{{.Name}}-{{.Tag}}",
		want: map[string]string{
			"myroute-target-1.default.example.com": "target-1",
			"myroute-target-2.default.example.com": "target-2",
			"myroute.default.example.com":          "",
		},
	}, {
		name:           "another happy case",
		domainTemplate: "{{.Name}}.{{.Namespace}}.{{.Domain}}",
		tagTemplate:    "{{.Tag}}-{{.Name}}",
		want: map[string]string{
			"target-1-myroute.default.example.com": "target-1",
			"target-2-myroute.default.example.com": "target-2",
			"myroute.default.example.com":          "",
		},
	}, {
		name:           "or appengine style",
		domainTemplate: "{{.Name}}.{{.Namespace}}.{{.Domain}}",
		tagTemplate:    "{{.Tag}}-dot-{{.Name}}",
		want: map[string]string{
			"target-1-dot-myroute.default.example.com": "target-1",
			"target-2-dot-myroute.default.example.com": "target-2",
			"myroute.default.example.com":              "",
		},
	}, {
		name:           "bad template",
		domainTemplate: "{{.NNName}}.{{.Namespace}}.{{.Domain}}",
		tagTemplate:    "{{.Name}}-{{.Tag}}",
		wantErr:        true,
	}, {
		name:           "bad template",
		domainTemplate: "{{.Name}}.{{.Namespace}}.{{.Domain}}",
		tagTemplate:    "{{.NNName}}-{{.Tag}}",
		wantErr:        true,
	}}

	route := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/serving/v1alpha1/namespaces/test/Routes/myapp",
			Name:      "myroute",
			Namespace: "default",
			Labels: map[string]string{
				"route": "myapp",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := testConfig()
			cfg.Network.DomainTemplate = tt.domainTemplate
			cfg.Network.TagTemplate = tt.tagTemplate
			ctx = config.ToContext(ctx, cfg)

			// here, a tag-less major domain will have empty string as the input
			got, err := GetAllDomainsAndTags(ctx, route, []string{"", "target-1", "target-2"})
			if (err != nil) != tt.wantErr {
				t.Errorf("GetAllDomains() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("GetAllDomains() diff (-want +got): %v", diff)
			}
		})
	}
}
