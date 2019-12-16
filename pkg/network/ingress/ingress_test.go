/*
Copyright 2019 The Knative Authors.

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

package ingress

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
)

func TestGetExpandedHosts(t *testing.T) {
	for _, test := range []struct {
		name  string
		hosts sets.String
		want  sets.String
	}{{
		name: "cluster local service in non-default namespace",
		hosts: sets.NewString(
			"service.namespace.svc.cluster.local",
		),
		want: sets.NewString(
			"service.namespace",
			"service.namespace.svc",
			"service.namespace.svc.cluster.local",
		),
	}, {
		name: "example.com service",
		hosts: sets.NewString(
			"foo.bar.example.com",
		),
		want: sets.NewString(
			"foo.bar.example.com",
		),
	}, {
		name: "default.example.com service",
		hosts: sets.NewString(
			"foo.default.example.com",
		),
		want: sets.NewString(
			"foo.default.example.com",
		),
	}, {
		name: "mix",
		hosts: sets.NewString(
			"foo.default.example.com",
			"foo.default.svc.cluster.local",
		),
		want: sets.NewString(
			"foo.default",
			"foo.default.example.com",
			"foo.default.svc",
			"foo.default.svc.cluster.local",
		),
	}} {
		t.Run(test.name, func(t *testing.T) {
			got := ExpandedHosts(test.hosts)
			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("Unexpected (-want +got): %v", diff)
			}
		})
	}
}

func TestInsertProbe(t *testing.T) {
	tests := []struct {
		name    string
		ingress *v1alpha1.Ingress
		want    string
	}{{
		name: "with rules, no append header",
		ingress: &v1alpha1.Ingress{
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"example.com",
					},
					HTTP: &v1alpha1.HTTPIngressRuleValue{
						Paths: []v1alpha1.HTTPIngressPath{{
							Splits: []v1alpha1.IngressBackendSplit{{
								IngressBackend: v1alpha1.IngressBackend{
									ServiceName: "blah",
								},
							}},
						}},
					},
				}},
			},
		},
		want: "b90f793b72c245476c6b4060967121ef",
	}, {
		name: "with rules, with append header",
		ingress: &v1alpha1.Ingress{
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"example.com",
					},
					HTTP: &v1alpha1.HTTPIngressRuleValue{
						Paths: []v1alpha1.HTTPIngressPath{{
							Splits: []v1alpha1.IngressBackendSplit{{
								IngressBackend: v1alpha1.IngressBackend{
									ServiceName: "blah",
								},
								AppendHeaders: map[string]string{
									"Foo": "bar",
								},
							}},
						}},
					},
				}},
			},
		},
		want: "061575cdf950105126a81d6da83cda8b",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := len(test.ingress.Spec.Rules[0].HTTP.Paths[0].AppendHeaders)
			got, err := InsertProbe(test.ingress)
			if err != nil {
				t.Errorf("InsertProbe() = %v", err)
			}
			if got != test.want {
				t.Errorf("InsertProbe() = %s, wanted %s", got, test.want)
			}
			after := len(test.ingress.Spec.Rules[0].HTTP.Paths[0].AppendHeaders)
			if before+1 != after {
				t.Errorf("InsertProbe() left %d headers, wanted %d", after, before+1)
			}
		})
	}
}

func TestHostsPerVisibility(t *testing.T) {
	tests := []struct {
		name    string
		ingress *v1alpha1.Ingress
		in      map[v1alpha1.IngressVisibility]sets.String
		want    map[string]sets.String
	}{{
		name: "external rule",
		ingress: &v1alpha1.Ingress{
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"example.com",
						"foo.bar.svc.cluster.local",
					},
					HTTP: &v1alpha1.HTTPIngressRuleValue{
						Paths: []v1alpha1.HTTPIngressPath{{
							Splits: []v1alpha1.IngressBackendSplit{{
								IngressBackend: v1alpha1.IngressBackend{
									ServiceName: "blah",
								},
								AppendHeaders: map[string]string{
									"Foo": "bar",
								},
							}},
						}},
					},
					Visibility: v1alpha1.IngressVisibilityExternalIP,
				}},
			},
		},
		in: map[v1alpha1.IngressVisibility]sets.String{
			v1alpha1.IngressVisibilityExternalIP:   sets.NewString("foo"),
			v1alpha1.IngressVisibilityClusterLocal: sets.NewString("bar", "baz"),
		},
		want: map[string]sets.String{
			"foo": sets.NewString(
				"example.com",
				"foo.bar.svc.cluster.local",
				"foo.bar.svc",
				"foo.bar",
			),
		},
	}, {
		name: "internal rule",
		ingress: &v1alpha1.Ingress{
			Spec: v1alpha1.IngressSpec{
				Rules: []v1alpha1.IngressRule{{
					Hosts: []string{
						"foo.bar.svc.cluster.local",
					},
					HTTP: &v1alpha1.HTTPIngressRuleValue{
						Paths: []v1alpha1.HTTPIngressPath{{
							Splits: []v1alpha1.IngressBackendSplit{{
								IngressBackend: v1alpha1.IngressBackend{
									ServiceName: "blah",
								},
								AppendHeaders: map[string]string{
									"Foo": "bar",
								},
							}},
						}},
					},
					Visibility: v1alpha1.IngressVisibilityClusterLocal,
				}},
			},
		},
		in: map[v1alpha1.IngressVisibility]sets.String{
			v1alpha1.IngressVisibilityExternalIP:   sets.NewString("foo"),
			v1alpha1.IngressVisibilityClusterLocal: sets.NewString("bar", "baz"),
		},
		want: map[string]sets.String{
			"bar": sets.NewString(
				"foo.bar.svc.cluster.local",
				"foo.bar.svc",
				"foo.bar",
			),
			"baz": sets.NewString(
				"foo.bar.svc.cluster.local",
				"foo.bar.svc",
				"foo.bar",
			),
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := HostsPerVisibility(test.ingress, test.in)
			if !cmp.Equal(got, test.want) {
				t.Errorf("HostsPerVisibility (-want, +got) = %s", cmp.Diff(test.want, got))
			}
		})
	}
}
