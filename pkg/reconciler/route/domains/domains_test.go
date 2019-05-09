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

func TestSubdomainName(t *testing.T) {
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

	tests := []struct {
		name   string
		suffix string
		want   string
	}{
		{
			name:   "has suffix",
			suffix: "mysuffix",
			want:   "myroute-mysuffix",
		}, {
			name: "no suffix",
			want: "myroute",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SubdomainName(route, tt.suffix); got != tt.want {
				t.Errorf("BuildName() = %v, want %v", got, tt.want)
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
			Path:   "current.svc.local.com",
		},
	}, {
		name:   "default target",
		scheme: HTTPScheme,
		domain: "example.com",
		Expected: apis.URL{
			Scheme: "http",
			Path:   "example.com",
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

func TestGetAllDomains(t *testing.T) {
	tests := []struct {
		name     string
		template string
		want     []string
		wantErr  bool
	}{{
		name:     "happy case",
		template: "{{.Name}}.{{.Namespace}}.{{.Domain}}",
		want:     []string{"myroute-target-1.default.example.com", "myroute-target-2.default.example.com", "myroute.default.example.com"},
	}, {
		name:     "bad template",
		template: "{{.NNName}}.{{.Namespace}}.{{.Domain}}",
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
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := testConfig()
			cfg.Network.DomainTemplate = tt.template
			ctx = config.ToContext(ctx, cfg)

			got, err := GetAllDomains(ctx, route, []string{"target-1", "target-2"})
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
