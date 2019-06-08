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

package v1alpha1

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/apis"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestIngressSpecValidation(t *testing.T) {
	tests := []struct {
		name string
		is   *IngressSpec
		want *apis.FieldError
	}{{
		name: "valid",
		is: &IngressSpec{
			TLS: []IngressTLS{{
				SecretNamespace: "secret-space",
				SecretName:      "secret-name",
			}},
			Rules: []IngressRule{{
				Hosts: []string{"example.com"},
				HTTP: &HTTPIngressRuleValue{
					Paths: []HTTPIngressPath{{
						Splits: []IngressBackendSplit{{
							IngressBackend: IngressBackend{
								ServiceName:      "revision-000",
								ServiceNamespace: "default",
								ServicePort:      intstr.FromInt(8080),
							},
						}},
						Retries: &HTTPRetry{
							Attempts: 3,
						},
					}},
				},
			}},
		},
		want: nil,
	}, {
		name: "empty",
		is:   &IngressSpec{},
		want: apis.ErrMissingField(apis.CurrentField),
	}, {
		name: "missing-rule",
		is: &IngressSpec{
			TLS: []IngressTLS{{
				SecretName:      "secret-name",
				SecretNamespace: "secret-namespace",
			}},
		},
		want: apis.ErrMissingField("rules"),
	}, {
		name: "empty-rule",
		is: &IngressSpec{
			Rules: []IngressRule{{}},
		},
		want: apis.ErrMissingField("rules[0]"),
	}, {
		name: "missing-http",
		is: &IngressSpec{
			Rules: []IngressRule{{
				Hosts: []string{"example.com"},
			}},
		},
		want: apis.ErrMissingField("rules[0].http"),
	}, {
		name: "missing-http-paths",
		is: &IngressSpec{
			Rules: []IngressRule{{
				Hosts: []string{"example.com"},
				HTTP:  &HTTPIngressRuleValue{},
			}},
		},
		want: apis.ErrMissingField("rules[0].http.paths"),
	}, {
		name: "empty-http-paths",
		is: &IngressSpec{
			Rules: []IngressRule{{
				Hosts: []string{"example.com"},
				HTTP: &HTTPIngressRuleValue{
					Paths: []HTTPIngressPath{{}},
				},
			}},
		},
		want: apis.ErrMissingField("rules[0].http.paths[0]"),
	}, {
		name: "backend-wrong-percentage",
		is: &IngressSpec{
			Rules: []IngressRule{{
				Hosts: []string{"example.com"},
				HTTP: &HTTPIngressRuleValue{
					Paths: []HTTPIngressPath{{
						Splits: []IngressBackendSplit{{
							IngressBackend: IngressBackend{
								ServiceName:      "revision-000",
								ServiceNamespace: "default",
								ServicePort:      intstr.FromInt(8080),
							},
							Percent: 199,
						}},
					}},
				},
			}},
		},
		want: apis.ErrInvalidValue(199, "rules[0].http.paths[0].splits[0].percent"),
	}, {
		name: "missing-split",
		is: &IngressSpec{
			Rules: []IngressRule{{
				Hosts: []string{"example.com"},
				HTTP: &HTTPIngressRuleValue{
					Paths: []HTTPIngressPath{{
						Splits: []IngressBackendSplit{},
						AppendHeaders: map[string]string{
							"foo": "bar",
						},
					}},
				},
			}},
		},
		want: apis.ErrMissingField("rules[0].http.paths[0].splits"),
	}, {
		name: "empty-split",
		is: &IngressSpec{
			Rules: []IngressRule{{
				Hosts: []string{"example.com"},
				HTTP: &HTTPIngressRuleValue{
					Paths: []HTTPIngressPath{{
						Splits: []IngressBackendSplit{{}},
						AppendHeaders: map[string]string{
							"foo": "bar",
						},
					}},
				},
			}},
		},
		want: apis.ErrMissingField("rules[0].http.paths[0].splits[0]"),
	}, {
		name: "missing-split-backend",
		is: &IngressSpec{
			Rules: []IngressRule{{
				Hosts: []string{"example.com"},
				HTTP: &HTTPIngressRuleValue{
					Paths: []HTTPIngressPath{{
						Splits: []IngressBackendSplit{{
							IngressBackend: IngressBackend{},
							Percent:        100,
						}},
					}},
				},
			}},
		},
		want: apis.ErrMissingField("rules[0].http.paths[0].splits[0]"),
	}, {
		name: "missing-backend-name",
		is: &IngressSpec{
			Rules: []IngressRule{{
				Hosts: []string{"example.com"},
				HTTP: &HTTPIngressRuleValue{
					Paths: []HTTPIngressPath{{
						Splits: []IngressBackendSplit{{
							IngressBackend: IngressBackend{
								ServiceNamespace: "default",
								ServicePort:      intstr.FromInt(8080),
							},
						}},
					}},
				},
			}},
		},
		want: apis.ErrMissingField("rules[0].http.paths[0].splits[0].serviceName"),
	}, {
		name: "missing-backend-namespace",
		is: &IngressSpec{
			Rules: []IngressRule{{
				Hosts: []string{"example.com"},
				HTTP: &HTTPIngressRuleValue{
					Paths: []HTTPIngressPath{{
						Splits: []IngressBackendSplit{{
							IngressBackend: IngressBackend{
								ServiceName: "service-name",
								ServicePort: intstr.FromInt(8080),
							},
						}},
					}},
				},
			}},
		},
		want: apis.ErrMissingField("rules[0].http.paths[0].splits[0].serviceNamespace"),
	}, {
		name: "missing-backend-port",
		is: &IngressSpec{
			Rules: []IngressRule{{
				Hosts: []string{"example.com"},
				HTTP: &HTTPIngressRuleValue{
					Paths: []HTTPIngressPath{{
						Splits: []IngressBackendSplit{{
							IngressBackend: IngressBackend{
								ServiceName:      "service-name",
								ServiceNamespace: "default",
							},
						}},
					}},
				},
			}},
		},
		want: apis.ErrMissingField("rules[0].http.paths[0].splits[0].servicePort"),
	}, {
		name: "split-percent-sum-not-100",
		is: &IngressSpec{
			Rules: []IngressRule{{
				Hosts: []string{"example.com"},
				HTTP: &HTTPIngressRuleValue{
					Paths: []HTTPIngressPath{{
						Splits: []IngressBackendSplit{{
							IngressBackend: IngressBackend{
								ServiceName:      "revision-000",
								ServiceNamespace: "default",
								ServicePort:      intstr.FromInt(8080),
							},
							Percent: 30,
						}},
					}},
				},
			}},
		},
		want: &apis.FieldError{
			Message: "Traffic split percentage must total to 100, but was 30",
			Paths:   []string{"rules[0].http.paths[0].splits"},
		},
	}, {
		name: "wrong-retry-attempts",
		is: &IngressSpec{
			Rules: []IngressRule{{
				Hosts: []string{"example.com"},
				HTTP: &HTTPIngressRuleValue{
					Paths: []HTTPIngressPath{{
						Splits: []IngressBackendSplit{{
							IngressBackend: IngressBackend{
								ServiceName:      "revision-000",
								ServiceNamespace: "default",
								ServicePort:      intstr.FromInt(8080),
							},
						}},
						Retries: &HTTPRetry{
							Attempts: -1,
						},
					}},
				},
			}},
		},
		want: apis.ErrInvalidValue(-1, "rules[0].http.paths[0].retries.attempts"),
	}, {
		name: "empty-tls",
		is: &IngressSpec{
			TLS: []IngressTLS{{}},
			Rules: []IngressRule{{
				Hosts: []string{"example.com"},
				HTTP: &HTTPIngressRuleValue{
					Paths: []HTTPIngressPath{{
						Splits: []IngressBackendSplit{{
							IngressBackend: IngressBackend{
								ServiceName:      "revision-000",
								ServiceNamespace: "default",
								ServicePort:      intstr.FromInt(8080),
							},
						}},
					}},
				},
			}},
		},
		want: apis.ErrMissingField("tls[0]"),
	}, {
		name: "missing-tls-secret-namespace",
		is: &IngressSpec{
			TLS: []IngressTLS{{
				SecretName: "secret",
			}},
			Rules: []IngressRule{{
				Hosts: []string{"example.com"},
				HTTP: &HTTPIngressRuleValue{
					Paths: []HTTPIngressPath{{
						Splits: []IngressBackendSplit{{
							IngressBackend: IngressBackend{
								ServiceName:      "revision-000",
								ServiceNamespace: "default",
								ServicePort:      intstr.FromInt(8080),
							},
						}},
					}},
				},
			}},
		},
		want: apis.ErrMissingField("tls[0].secretNamespace"),
	}, {
		name: "missing-tls-secret-name",
		is: &IngressSpec{
			TLS: []IngressTLS{{
				SecretNamespace: "secret-space",
			}},
			Rules: []IngressRule{{
				Hosts: []string{"example.com"},
				HTTP: &HTTPIngressRuleValue{
					Paths: []HTTPIngressPath{{
						Splits: []IngressBackendSplit{{
							IngressBackend: IngressBackend{
								ServiceName:      "revision-000",
								ServiceNamespace: "default",
								ServicePort:      intstr.FromInt(8080),
							},
						}},
					}},
				},
			}},
		},
		want: apis.ErrMissingField("tls[0].secretName"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.is.Validate(context.Background())
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}
func TestIngressValidation(t *testing.T) {
	tests := []struct {
		name string
		ci   *Ingress
		want *apis.FieldError
	}{{
		name: "valid",
		ci: &Ingress{
			Spec: IngressSpec{
				TLS: []IngressTLS{{
					SecretNamespace: "secret-space",
					SecretName:      "secret-name",
				}},
				Rules: []IngressRule{{
					Hosts: []string{"example.com"},
					HTTP: &HTTPIngressRuleValue{
						Paths: []HTTPIngressPath{{
							Splits: []IngressBackendSplit{{
								IngressBackend: IngressBackend{
									ServiceName:      "revision-000",
									ServiceNamespace: "default",
									ServicePort:      intstr.FromInt(8080),
								},
							}},
							Retries: &HTTPRetry{
								Attempts: 3,
							},
						}},
					},
				}},
			},
		},
		want: nil,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.ci.Validate(context.Background())
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}
