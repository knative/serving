/*
Copyright 2020 The Knative Authors

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

package visibility

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	listers "k8s.io/client-go/listers/core/v1"
	networking "knative.dev/networking/pkg"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/network"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/traffic"
)

func getContext(domainSuffix string) context.Context {
	if domainSuffix == "" {
		domainSuffix = "example.com"
	}
	return config.ToContext(context.Background(), &config.Config{
		Domain: &config.Domain{
			Domains: map[string]*config.LabelSelector{
				domainSuffix: {},
			},
		},
		Network: &networking.Config{
			TagTemplate:    networking.DefaultTagTemplate,
			DomainTemplate: networking.DefaultDomainTemplate,
		},
	})
}

func TestVisibility(t *testing.T) {
	listerErr := errors.New("lister error")
	for _, tt := range []struct {
		name         string
		domainSuffix string
		services     []*corev1.Service
		listerErr    error
		route        *v1.Route
		expected     map[string]netv1alpha1.IngressVisibility
		expectedErr  error
	}{{
		name: "default",
		route: &v1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
		},
		expected: map[string]netv1alpha1.IngressVisibility{
			traffic.DefaultTarget: netv1alpha1.IngressVisibilityExternalIP,
		},
	}, {
		name: "no tag, route marked local",
		route: &v1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				Labels: map[string]string{
					networking.VisibilityLabelKey: serving.VisibilityClusterLocal,
				},
			},
		},
		expected: map[string]netv1alpha1.IngressVisibility{
			traffic.DefaultTarget: netv1alpha1.IngressVisibilityClusterLocal,
		},
	}, {
		name: "no tag, svc marked local",
		route: &v1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
		},
		services: []*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				Labels: map[string]string{
					serving.RouteLabelKey:         "foo",
					networking.VisibilityLabelKey: serving.VisibilityClusterLocal,
				},
			},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Name: "irrelevance",
				Labels: map[string]string{
					serving.RouteLabelKey:         "bar",
					networking.VisibilityLabelKey: serving.VisibilityClusterLocal,
				},
			},
		}},
		expected: map[string]netv1alpha1.IngressVisibility{
			traffic.DefaultTarget: netv1alpha1.IngressVisibilityClusterLocal,
		},
	}, {
		name: "one tag, tag marked local",
		route: &v1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{Tag: "blue"}},
			},
		},
		services: []*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{
				Name: "blue-foo",
				Labels: map[string]string{
					serving.RouteLabelKey:         "foo",
					networking.VisibilityLabelKey: serving.VisibilityClusterLocal,
				},
			},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				Labels: map[string]string{
					serving.RouteLabelKey: "foo",
				},
			},
		}},
		expected: map[string]netv1alpha1.IngressVisibility{
			traffic.DefaultTarget: netv1alpha1.IngressVisibilityExternalIP,
			"blue":                netv1alpha1.IngressVisibilityClusterLocal,
		},
	}, {
		name: "one tag initial default",
		route: &v1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{Tag: "blue"}},
			},
		},
		services: []*corev1.Service{},
		expected: map[string]netv1alpha1.IngressVisibility{
			traffic.DefaultTarget: netv1alpha1.IngressVisibilityExternalIP,
			"blue":                netv1alpha1.IngressVisibilityExternalIP,
		},
	}, {
		name: "one tag svc not marked",
		route: &v1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{Tag: "blue"}},
			},
		},
		services: []*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{
				Name: "blue-foo",
				Labels: map[string]string{
					serving.RouteLabelKey: "foo",
				},
			},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				Labels: map[string]string{
					serving.RouteLabelKey: "foo",
				},
			},
		}},
		expected: map[string]netv1alpha1.IngressVisibility{
			traffic.DefaultTarget: netv1alpha1.IngressVisibilityExternalIP,
			"blue":                netv1alpha1.IngressVisibilityExternalIP,
		},
	}, {
		name: "two tags initial default",
		route: &v1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{
					Tag: "blue",
				}, {
					Tag: "green",
				}},
			},
		},
		expected: map[string]netv1alpha1.IngressVisibility{
			traffic.DefaultTarget: netv1alpha1.IngressVisibilityExternalIP,
			"blue":                netv1alpha1.IngressVisibilityExternalIP,
			"green":               netv1alpha1.IngressVisibilityExternalIP,
		},
	}, {
		name: "two tags initial default with cluster domain suffix",
		route: &v1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{
					Tag: "blue",
				}, {
					Tag: "green",
				}},
			},
		},
		domainSuffix: "svc." + network.GetClusterDomainName(),
		expected: map[string]netv1alpha1.IngressVisibility{
			traffic.DefaultTarget: netv1alpha1.IngressVisibilityClusterLocal,
			"blue":                netv1alpha1.IngressVisibilityClusterLocal,
			"green":               netv1alpha1.IngressVisibilityClusterLocal,
		},
	}, {
		name: "two tags, svc not marked",
		route: &v1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{
					Tag: "blue",
				}, {
					Tag: "green",
				}},
			},
		},
		services: []*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{
				Name: "blue-foo",
				Labels: map[string]string{
					serving.RouteLabelKey: "foo",
				},
			},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Name: "green-foo",
				Labels: map[string]string{
					serving.RouteLabelKey: "foo",
				},
			},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				Labels: map[string]string{
					serving.RouteLabelKey: "foo",
				},
			},
		}},
		expected: map[string]netv1alpha1.IngressVisibility{
			traffic.DefaultTarget: netv1alpha1.IngressVisibilityExternalIP,
			"blue":                netv1alpha1.IngressVisibilityExternalIP,
			"green":               netv1alpha1.IngressVisibilityExternalIP,
		},
	}, {
		name: "two tags, route marked local",
		route: &v1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				Labels: map[string]string{
					networking.VisibilityLabelKey: serving.VisibilityClusterLocal,
				},
			},
			Spec: v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{
					Tag: "blue",
				}, {
					Tag: "green",
				}},
			},
		},
		services: []*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{
				Name: "blue-foo",
				Labels: map[string]string{
					serving.RouteLabelKey: "foo",
				},
			},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Name: "green-foo",
				Labels: map[string]string{
					serving.RouteLabelKey: "foo",
				},
			},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				Labels: map[string]string{
					serving.RouteLabelKey: "foo",
				},
			},
		}},
		expected: map[string]netv1alpha1.IngressVisibility{
			traffic.DefaultTarget: netv1alpha1.IngressVisibilityClusterLocal,
			"blue":                netv1alpha1.IngressVisibilityClusterLocal,
			"green":               netv1alpha1.IngressVisibilityClusterLocal,
		},
	}, {
		name: "two tags blue marked local",
		route: &v1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{
					Tag: "blue",
				}, {
					Tag: "green",
				}},
			},
		},
		services: []*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{
				Name: "blue-foo",
				Labels: map[string]string{
					serving.RouteLabelKey:         "foo",
					networking.VisibilityLabelKey: serving.VisibilityClusterLocal,
				},
			},
		}},
		expected: map[string]netv1alpha1.IngressVisibility{
			traffic.DefaultTarget: netv1alpha1.IngressVisibilityExternalIP,
			"blue":                netv1alpha1.IngressVisibilityClusterLocal,
			"green":               netv1alpha1.IngressVisibilityExternalIP,
		},
	}, {
		name: "two tags, both marked local",
		route: &v1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{
					Tag: "blue",
				}, {
					Tag: "green",
				}},
			},
		},
		services: []*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{
				Name: "blue-foo",
				Labels: map[string]string{
					serving.RouteLabelKey:         "foo",
					networking.VisibilityLabelKey: serving.VisibilityClusterLocal,
				},
			},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Name: "green-foo",
				Labels: map[string]string{
					serving.RouteLabelKey:         "foo",
					networking.VisibilityLabelKey: serving.VisibilityClusterLocal,
				},
			},
		}},
		expected: map[string]netv1alpha1.IngressVisibility{
			traffic.DefaultTarget: netv1alpha1.IngressVisibilityExternalIP,
			"blue":                netv1alpha1.IngressVisibilityClusterLocal,
			"green":               netv1alpha1.IngressVisibilityClusterLocal,
		},
	}, {
		name: "two tags, all marked local",
		route: &v1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
			Spec: v1.RouteSpec{
				Traffic: []v1.TrafficTarget{{
					Tag: "blue",
				}, {
					Tag: "green",
				}},
			},
		},
		services: []*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{
				Name: "blue-foo",
				Labels: map[string]string{
					serving.RouteLabelKey:         "foo",
					networking.VisibilityLabelKey: serving.VisibilityClusterLocal,
				},
			},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Name: "green-foo",
				Labels: map[string]string{
					serving.RouteLabelKey:         "foo",
					networking.VisibilityLabelKey: serving.VisibilityClusterLocal,
				},
			},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				Labels: map[string]string{
					serving.RouteLabelKey:         "foo",
					networking.VisibilityLabelKey: serving.VisibilityClusterLocal,
				},
			},
		}},
		expected: map[string]netv1alpha1.IngressVisibility{
			traffic.DefaultTarget: netv1alpha1.IngressVisibilityClusterLocal,
			"blue":                netv1alpha1.IngressVisibilityClusterLocal,
			"green":               netv1alpha1.IngressVisibilityClusterLocal,
		},
	}, {
		name:      "lister error",
		listerErr: listerErr,
		route: &v1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
		},
		expectedErr: listerErr,
	}} {
		t.Run(tt.name, func(t *testing.T) {
			lister := &fakeServiceLister{services: tt.services, listerErr: tt.listerErr}
			ctx := getContext(tt.domainSuffix)
			visibility, err := NewResolver(lister).GetVisibility(ctx, tt.route)
			if got, want := visibility, tt.expected; !cmp.Equal(got, want) {
				t.Errorf("Unexpected visibility diff (-want +got):\n%s", cmp.Diff(want, got))
			}
			if tt.expectedErr != err {
				t.Errorf("Err = %v, want: %v", err, tt.expectedErr)
			}
		})
	}
}

type fakeServiceLister struct {
	listers.ServiceNamespaceLister
	services  []*corev1.Service
	listerErr error
}

func (l *fakeServiceLister) List(selector labels.Selector) ([]*corev1.Service, error) {
	if l.listerErr != nil {
		return nil, l.listerErr
	}
	results := []*corev1.Service{}
	for _, svc := range l.services {
		if selector.Matches(labels.Set(svc.Labels)) {
			results = append(results, svc)
		}
	}
	return results, nil
}

func (l *fakeServiceLister) Services(namespace string) listers.ServiceNamespaceLister {
	return l
}
