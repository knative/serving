/*
Copyright 2018 The Knative Authors.

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

package network

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	lru "github.com/hashicorp/golang-lru"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/system"

	. "knative.dev/pkg/configmap/testing"
	_ "knative.dev/pkg/system/testing"
)

func TestOurConfig(t *testing.T) {
	cm, example := ConfigMapsFromTestFile(t, ConfigName)

	if _, err := NewConfigFromConfigMap(cm); err != nil {
		t.Errorf("NewConfigFromConfigMap(actual) = %v", err)
	}
	if got, err := NewConfigFromConfigMap(example); err != nil {
		t.Errorf("NewConfigFromConfigMap(example) = %v", err)
	} else if want := defaultConfig(); !cmp.Equal(got, want) {
		t.Errorf("ExampleConfig does not match default confif: (-want,+got):\n%s", cmp.Diff(want, got))
	}
}

func TestConfiguration(t *testing.T) {
	const nonDefaultDomainTemplate = "{{.Namespace}}.{{.Name}}.{{.Domain}}"
	ignoreDT := cmpopts.IgnoreFields(Config{}, "DomainTemplate")

	networkConfigTests := []struct {
		name       string
		wantErr    bool
		wantConfig *Config
		data       map[string]string
	}{{
		name:       "network configuration with no network input",
		wantErr:    false,
		wantConfig: defaultConfig(),
	}, {
		name: "network configuration with non-default ingress type",
		data: map[string]string{
			DefaultIngressClassKey: "foo-ingress",
		},
		wantErr: false,
		wantConfig: func() *Config {
			c := defaultConfig()
			c.DefaultIngressClass = "foo-ingress"
			return c
		}(),
	}, {
		name: "network configuration with non-Cert-Manager Certificate type",
		data: map[string]string{
			DefaultCertificateClassKey: "foo-cert",
		},
		wantErr: false,
		wantConfig: func() *Config {
			c := defaultConfig()
			c.DefaultCertificateClass = "foo-cert"
			return c
		}(),
	}, {
		name: "network configuration with diff domain template",
		data: map[string]string{
			DefaultIngressClassKey: "foo-ingress",
			DomainTemplateKey:      nonDefaultDomainTemplate,
		},
		wantErr: false,
		wantConfig: func() *Config {
			c := defaultConfig()
			c.DefaultIngressClass = "foo-ingress"
			c.DomainTemplate = nonDefaultDomainTemplate
			return c
		}(),
	}, {
		name:    "network configuration with blank domain template",
		wantErr: true,
		data: map[string]string{
			DefaultIngressClassKey: "foo-ingress",
			DomainTemplateKey:      "",
		},
	}, {
		name:    "network configuration with bad domain template",
		wantErr: true,
		data: map[string]string{
			DefaultIngressClassKey: "foo-ingress",
			// This is missing a closing brace.
			DomainTemplateKey: "{{.Namespace}.{{.Name}}.{{.Domain}}",
		},
	}, {
		name:    "network configuration with bad domain template",
		wantErr: true,
		data: map[string]string{
			DefaultIngressClassKey: "foo-ingress",
			// This is missing a closing brace.
			DomainTemplateKey: "{{.Namespace}.{{.Name}}.{{.Domain}}",
		},
	}, {
		name:    "network configuration with bad url",
		wantErr: true,
		data: map[string]string{
			DefaultIngressClassKey: "foo-ingress",
			// Paths are disallowed
			DomainTemplateKey: "{{.Domain}}/{{.Namespace}}/{{.Name}}.",
		},
	}, {
		name:    "network configuration with bad variable",
		wantErr: true,
		data: map[string]string{
			DefaultIngressClassKey: "foo-ingress",
			// Bad variable
			DomainTemplateKey: "{{.Name}}.{{.NAmespace}}.{{.Domain}}",
		},
	}, {
		name: "network configuration with Auto TLS enabled",
		data: map[string]string{
			AutoTLSKey: "enabled",
		},
		wantErr: false,
		wantConfig: func() *Config {
			c := defaultConfig()
			c.AutoTLS = true
			return c
		}(),
	}, {
		name: "network configuration with Auto TLS disabled",
		data: map[string]string{
			AutoTLSKey: "disabled",
		},
		wantErr:    false,
		wantConfig: defaultConfig(),
	}, {
		name: "network configuration with HTTPProtocol disabled",
		data: map[string]string{
			AutoTLSKey:      "enabled",
			HTTPProtocolKey: "Disabled",
		},
		wantErr: false,
		wantConfig: func() *Config {
			c := defaultConfig()
			c.AutoTLS = true
			c.HTTPProtocol = HTTPDisabled
			return c
		}(),
	}, {
		name: "network configuration with HTTPProtocol redirected",
		data: map[string]string{
			AutoTLSKey:      "enabled",
			HTTPProtocolKey: "Redirected",
		},
		wantErr: false,
		wantConfig: func() *Config {
			c := defaultConfig()
			c.AutoTLS = true
			c.HTTPProtocol = HTTPRedirected
			return c
		}(),
	}, {
		name: "network configuration with HTTPProtocol bad",
		data: map[string]string{
			AutoTLSKey:      "enabled",
			HTTPProtocolKey: "under-the-bridge",
		},
		wantErr: true,
	}}

	for _, tt := range networkConfigTests {
		t.Run(tt.name, func(t *testing.T) {
			config := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: system.Namespace(),
					Name:      ConfigName,
				},
				Data: tt.data,
			}
			actualConfig, err := NewConfigFromConfigMap(config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Test: %q; NewConfigFromConfigMap() error = %v, WantErr %v",
					tt.name, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			data := DomainTemplateValues{
				Name:      "foo",
				Namespace: "bar",
				Domain:    "baz.com",
			}
			want := mustExecute(t, tt.wantConfig.GetDomainTemplate(), data)
			got := mustExecute(t, actualConfig.GetDomainTemplate(), data)
			if got != want {
				t.Errorf("DomainTemplate(data) = %s, wanted %s", got, want)
			}

			if diff := cmp.Diff(actualConfig, tt.wantConfig, ignoreDT); diff != "" {
				t.Fatalf("want %v, but got %v",
					tt.wantConfig, actualConfig)
			}
		})
	}
}

func TestTemplateCaching(t *testing.T) {
	// Reset the template cache, to ensure size change.
	templateCache, _ = lru.New(10)

	const anotherTemplate = "{{.Namespace}}.{{.Name}}.{{.Domain}}.sad"
	actualConfig, err := NewConfigFromMap(map[string]string{
		DomainTemplateKey: anotherTemplate,
	})
	if err != nil {
		t.Fatalf("Config parsing failure = %v", err)
	}
	if got, want := actualConfig.DomainTemplate, anotherTemplate; got != want {
		t.Errorf("DomainTemplate = %q, want: %q", got, want)
	}
	if got, want := templateCache.Len(), 2; got != want {
		t.Errorf("Cache size = %d, want = %d", got, want)
	}

	// Reset to default. And make sure it is cached.
	actualConfig, err = NewConfigFromMap(map[string]string{})
	if err != nil {
		t.Fatalf("Config parsing failure = %v", err)
	}

	if got, want := actualConfig.DomainTemplate, DefaultDomainTemplate; got != want {
		t.Errorf("DomainTemplate = %q, want: %q", got, want)
	}
	if got, want := templateCache.Len(), 3; got != want {
		t.Errorf("Cache size = %d, want = %d", got, want)
	}
}

func TestAnnotationsInDomainTemplate(t *testing.T) {
	networkConfigTests := []struct {
		name               string
		wantErr            bool
		wantDomainTemplate string
		config             *corev1.ConfigMap
		data               DomainTemplateValues
	}{{
		name:               "network configuration with annotations in template",
		wantErr:            false,
		wantDomainTemplate: "foo.sub1.baz.com",
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
			Data: map[string]string{
				DefaultIngressClassKey: "foo-ingress",
				DomainTemplateKey:      `{{.Name}}.{{ index .Annotations "sub"}}.{{.Domain}}`,
			},
		},
		data: DomainTemplateValues{
			Name:      "foo",
			Namespace: "bar",
			Annotations: map[string]string{
				"sub": "sub1"},
			Domain: "baz.com"},
	}, {
		name:               "network configuration without annotations in template",
		wantErr:            false,
		wantDomainTemplate: "foo.bar.baz.com",
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
			Data: map[string]string{
				DefaultIngressClassKey: "foo-ingress",
				DomainTemplateKey:      `{{.Name}}.{{.Namespace}}.{{.Domain}}`,
			},
		},
		data: DomainTemplateValues{
			Name:      "foo",
			Namespace: "bar",
			Domain:    "baz.com"},
	}}

	for _, tt := range networkConfigTests {
		t.Run(tt.name, func(t *testing.T) {
			actualConfig, err := NewConfigFromConfigMap(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Test: %q; NewConfigFromConfigMap() error = %v, WantErr %v",
					tt.name, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			got := mustExecute(t, actualConfig.GetDomainTemplate(), tt.data)
			if got != tt.wantDomainTemplate {
				t.Errorf("DomainTemplate(data) = %s, wanted %s", got, tt.wantDomainTemplate)
			}
		})
	}
}

func TestLabelsInDomainTemplate(t *testing.T) {
	networkConfigTests := []struct {
		name               string
		data               map[string]string
		templateValue      DomainTemplateValues
		wantErr            bool
		wantDomainTemplate string
	}{{
		name:               "network configuration with labels in template",
		wantErr:            false,
		wantDomainTemplate: "foo.sub1.baz.com",
		data: map[string]string{
			DefaultIngressClassKey: "foo-ingress",
			DomainTemplateKey:      `{{.Name}}.{{ index .Labels "sub"}}.{{.Domain}}`,
		},
		templateValue: DomainTemplateValues{
			Name:      "foo",
			Namespace: "bar",
			Labels: map[string]string{
				"sub": "sub1"},
			Domain: "baz.com"},
	}, {
		name:               "network configuration without labels in template",
		wantErr:            false,
		wantDomainTemplate: "foo.bar.baz.com",
		data: map[string]string{
			DefaultIngressClassKey: "foo-ingress",
			DomainTemplateKey:      `{{.Name}}.{{.Namespace}}.{{.Domain}}`,
		},
		templateValue: DomainTemplateValues{
			Name:      "foo",
			Namespace: "bar",
			Domain:    "baz.com"},
	}}

	for _, tt := range networkConfigTests {
		t.Run(tt.name, func(t *testing.T) {
			actualConfig, err := NewConfigFromConfigMap(&corev1.ConfigMap{
				Data: tt.data,
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewConfigFromConfigMap() error = %v, WantErr? %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			got := mustExecute(t, actualConfig.GetDomainTemplate(), tt.templateValue)
			if got != tt.wantDomainTemplate {
				t.Errorf("DomainTemplate(data) = %s, want: %s", got, tt.wantDomainTemplate)
			}
		})
	}
}

func mustExecute(t *testing.T, tmpl *template.Template, data interface{}) string {
	t.Helper()
	buf := bytes.Buffer{}
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Error("Error executing the DomainTemplate:", err)
	}
	return buf.String()
}

func TestIsKubeletProbe(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatal("Error building request:", err)
	}
	if IsKubeletProbe(req) {
		t.Error("Not a kubelet probe but counted as such")
	}
	req.Header.Set("User-Agent", KubeProbeUAPrefix+"1.14")
	if !IsKubeletProbe(req) {
		t.Error("kubelet probe but not counted as such")
	}
	req.Header.Del("User-Agent")
	if IsKubeletProbe(req) {
		t.Error("Not a kubelet probe but counted as such")
	}
	req.Header.Set(KubeletProbeHeaderName, "no matter")
	if !IsKubeletProbe(req) {
		t.Error("kubelet probe but not counted as such")
	}
}

func TestKnativeProbeHeader(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatal("Error building request:", err)
	}
	if h := KnativeProbeHeader(req); h != "" {
		t.Errorf("KnativeProbeHeader(req)=%v, want empty string", h)
	}
	const want = "activator"
	req.Header.Set(ProbeHeaderName, want)
	if h := KnativeProbeHeader(req); h != want {
		t.Errorf("KnativeProbeHeader(req)=%v, want %v", h, want)
	}
	req.Header.Set(ProbeHeaderName, "")
	if h := KnativeProbeHeader(req); h != "" {
		t.Errorf("KnativeProbeHeader(req)=%v, want empty string", h)
	}
}

func TestKnativeProxyHeader(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatal("Error building request:", err)
	}
	if h := KnativeProxyHeader(req); h != "" {
		t.Errorf("KnativeProxyHeader(req)=%v, want empty string", h)
	}
	const want = "activator"
	req.Header.Set(ProxyHeaderName, want)
	if h := KnativeProxyHeader(req); h != want {
		t.Errorf("KnativeProxyHeader(req)=%v, want %v", h, want)
	}
	req.Header.Set(ProxyHeaderName, "")
	if h := KnativeProxyHeader(req); h != "" {
		t.Errorf("KnativeProxyHeader(req)=%v, want empty string", h)
	}
}

func TestIsProbe(t *testing.T) {
	// Not a probe
	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatal("Error building request:", err)
	}
	if IsProbe(req) {
		t.Error("Not a probe but counted as such")
	}
	// Kubelet probe
	req.Header.Set("User-Agent", KubeProbeUAPrefix+"1.14")
	if !IsProbe(req) {
		t.Error("Kubelet probe but not counted as such")
	}
	// Knative probe
	req.Header.Del("User-Agent")
	req.Header.Set(ProbeHeaderName, "activator")
	if !IsProbe(req) {
		t.Error("Knative probe but not counted as such")
	}
}

func TestRewriteHost(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://love.is/not-hate", nil)
	r.Header.Set("Host", "love.is")

	RewriteHostIn(r)

	if got, want := r.Host, ""; got != want {
		t.Errorf("r.Host = %q, want: %q", got, want)
	}

	if got, want := r.Header.Get("Host"), ""; got != want {
		t.Errorf("r.Header['Host'] = %q, want: %q", got, want)
	}

	if got, want := r.Header.Get(OriginalHostHeader), "love.is"; got != want {
		t.Errorf("r.Header[%s] = %q, want: %q", OriginalHostHeader, got, want)
	}

	// Do it again, but make sure that the ORIGINAL domain is still preserved.
	r.Header.Set("Host", "hate.is")
	RewriteHostIn(r)

	if got, want := r.Host, ""; got != want {
		t.Errorf("r.Host = %q, want: %q", got, want)
	}

	if got, want := r.Header.Get("Host"), ""; got != want {
		t.Errorf("r.Header['Host'] = %q, want: %q", got, want)
	}

	if got, want := r.Header.Get(OriginalHostHeader), "love.is"; got != want {
		t.Errorf("r.Header[%s] = %q, want: %q", OriginalHostHeader, got, want)
	}

	RewriteHostOut(r)
	if got, want := r.Host, "love.is"; got != want {
		t.Errorf("r.Host = %q, want: %q", got, want)
	}

	if got, want := r.Header.Get("Host"), ""; got != want {
		t.Errorf("r.Header['Host'] = %q, want: %q", got, want)
	}

	if got, want := r.Header.Get(OriginalHostHeader), ""; got != want {
		t.Errorf("r.Header[%s] = %q, want: %q", OriginalHostHeader, got, want)
	}
}

func TestNameForPortNumber(t *testing.T) {
	for _, tc := range []struct {
		name       string
		svc        *corev1.Service
		portNumber int32
		portName   string
		err        error
	}{{
		name: "HTTP to 80",
		svc: &corev1.Service{
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
					Port: 80,
					Name: "http",
				}, {
					Port: 443,
					Name: "https",
				}},
			},
		},
		portName:   "http",
		portNumber: 80,
	}, {
		name: "no port",
		svc: &corev1.Service{
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
					Port: 443,
					Name: "https",
				}},
			},
		},
		portNumber: 80,
		err:        errors.New("no port with number 80 found"),
	}} {
		t.Run(tc.name, func(t *testing.T) {
			portName, err := NameForPortNumber(tc.svc, tc.portNumber)
			if !reflect.DeepEqual(err, tc.err) { // cmp Doesn't work well here due to private fields.
				t.Errorf("Err = %v, want: %v", err, tc.err)
			}
			if tc.err == nil && portName != tc.portName {
				t.Errorf("PortName = %s, want: %s", portName, tc.portName)
			}
		})
	}
}

func TestPortNumberForName(t *testing.T) {
	for _, tc := range []struct {
		name       string
		subset     corev1.EndpointSubset
		portNumber int32
		portName   string
		err        error
	}{{
		name: "HTTP to 80",
		subset: corev1.EndpointSubset{
			Ports: []corev1.EndpointPort{{
				Port: 8080,
				Name: "http",
			}, {
				Port: 8443,
				Name: "https",
			}},
		},
		portName:   "http",
		portNumber: 8080,
	}, {
		name: "no port",
		subset: corev1.EndpointSubset{
			Ports: []corev1.EndpointPort{{
				Port: 8443,
				Name: "https",
			}},
		},
		portName: "http",
		err:      errors.New(`no port for name "http" found`),
	}} {
		t.Run(tc.name, func(t *testing.T) {
			portNumber, err := PortNumberForName(tc.subset, tc.portName)
			if !reflect.DeepEqual(err, tc.err) { // cmp Doesn't work well here due to private fields.
				t.Errorf("Err = %v, want: %v", err, tc.err)
			}
			if tc.err == nil && portNumber != tc.portNumber {
				t.Errorf("PortNumber = %d, want: %d", portNumber, tc.portNumber)
			}
		})
	}
}
