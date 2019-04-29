/*
Copyright 2018 The Knative Authors

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

package test

// crd contains functions that construct boilerplate CRD definitions.

import (
	"strings"
	"testing"
	"unicode"

	"github.com/knative/pkg/ptr"
	ptest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/helpers"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	v1alpha1testing "github.com/knative/serving/pkg/reconciler/testing"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// Default for user containers in e2e tests. This value is lower than the general
	// Knative's default so as to run more effectively in CI with limited resources.
	defaultRequestCPU = "100m"

	testNamePrefix = "Test"
)

// ResourceNames holds names of various resources.
type ResourceNames struct {
	Config        string
	Route         string
	Revision      string
	Service       string
	TrafficTarget string
	Domain        string
	Image         string
}

// ResourceObjects holds types of the resource objects.
type ResourceObjects struct {
	Route    *v1alpha1.Route
	Config   *v1alpha1.Configuration
	Service  *v1alpha1.Service
	Revision *v1alpha1.Revision
}

// Route returns a Route object in namespace using the route and configuration
// names in names.
func Route(names ResourceNames, fopt ...v1alpha1testing.RouteOption) *v1alpha1.Route {
	route := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.Route,
		},
		Spec: v1alpha1.RouteSpec{
			Traffic: []v1alpha1.TrafficTarget{{
				TrafficTarget: v1beta1.TrafficTarget{
					Tag:               names.TrafficTarget,
					ConfigurationName: names.Config,
					Percent:           100,
				},
			}},
		},
	}

	for _, opt := range fopt {
		opt(route)
	}

	return route
}

// ConfigurationSpec returns the spec of a configuration to be used throughout different
// CRD helpers.
func ConfigurationSpec(imagePath string, options *Options) *v1alpha1.ConfigurationSpec {
	if options.ContainerResources.Limits == nil && options.ContainerResources.Requests == nil {
		options.ContainerResources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse(defaultRequestCPU),
			},
		}
	}

	spec := &v1alpha1.ConfigurationSpec{
		Template: &v1alpha1.RevisionTemplateSpec{
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					PodSpec: v1beta1.PodSpec{
						Containers: []corev1.Container{{
							Image:           imagePath,
							Resources:       options.ContainerResources,
							ReadinessProbe:  options.ReadinessProbe,
							Ports:           options.ContainerPorts,
							SecurityContext: options.SecurityContext,
						}},
					},
					ContainerConcurrency: v1beta1.RevisionContainerConcurrencyType(options.ContainerConcurrency),
				},
			},
		},
	}

	if options.RevisionTimeoutSeconds > 0 {
		spec.GetTemplate().Spec.TimeoutSeconds = ptr.Int64(options.RevisionTimeoutSeconds)
	}

	if options.EnvVars != nil {
		spec.GetTemplate().Spec.GetContainer().Env = options.EnvVars
	}

	return spec
}

// LegacyConfigurationSpec returns the spec of a configuration to be used throughout different
// CRD helpers.
func LegacyConfigurationSpec(imagePath string, options *Options) *v1alpha1.ConfigurationSpec {
	if options.ContainerResources.Limits == nil && options.ContainerResources.Requests == nil {
		options.ContainerResources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse(defaultRequestCPU),
			},
		}
	}

	spec := &v1alpha1.ConfigurationSpec{
		DeprecatedRevisionTemplate: &v1alpha1.RevisionTemplateSpec{
			Spec: v1alpha1.RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image:           imagePath,
					Resources:       options.ContainerResources,
					ReadinessProbe:  options.ReadinessProbe,
					Ports:           options.ContainerPorts,
					SecurityContext: options.SecurityContext,
				},
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: v1beta1.RevisionContainerConcurrencyType(options.ContainerConcurrency),
				},
			},
		},
	}

	if options.RevisionTimeoutSeconds > 0 {
		spec.GetTemplate().Spec.TimeoutSeconds = ptr.Int64(options.RevisionTimeoutSeconds)
	}

	if options.EnvVars != nil {
		spec.GetTemplate().Spec.GetContainer().Env = options.EnvVars
	}

	return spec
}

// Configuration returns a Configuration object in namespace with the name names.Config
// that uses the image specified by names.Image
func Configuration(names ResourceNames, options *Options, fopt ...v1alpha1testing.ConfigOption) *v1alpha1.Configuration {
	config := &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.Config,
		},
		Spec: *ConfigurationSpec(ptest.ImagePath(names.Image), options),
	}
	if options.ContainerPorts != nil && len(options.ContainerPorts) > 0 {
		config.Spec.GetTemplate().Spec.GetContainer().Ports = options.ContainerPorts
	}

	for _, opt := range fopt {
		opt(config)
	}

	return config
}

// ConfigurationWithBuild returns a Configuration object in the `namespace`
// with the name `names.Config` that uses the provided Build spec `build`
// and image specified by `names.Image`.
func ConfigurationWithBuild(names ResourceNames, build *v1alpha1.RawExtension) *v1alpha1.Configuration {
	return &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.Config,
		},
		Spec: v1alpha1.ConfigurationSpec{
			DeprecatedBuild: build,
			Template: &v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					RevisionSpec: v1beta1.RevisionSpec{
						PodSpec: v1beta1.PodSpec{
							Containers: []corev1.Container{{
								Image: ptest.ImagePath(names.Image),
							}},
						},
					},
				},
			},
		},
	}
}

// LatestService returns a Service object in namespace with the name names.Service
// that uses the image specified by names.Image.
func LatestService(names ResourceNames, options *Options, fopt ...v1alpha1testing.ServiceOption) *v1alpha1.Service {
	a := append([]v1alpha1testing.ServiceOption{
		v1alpha1testing.WithInlineConfigSpec(*ConfigurationSpec(ptest.ImagePath(names.Image), options)),
	}, fopt...)
	return v1alpha1testing.ServiceWithoutNamespace(names.Service, a...)
}

// LatestServiceLegacy returns a DeprecatedRunLatest Service object in namespace with the name names.Service
// that uses the image specified by names.Image.
func LatestServiceLegacy(names ResourceNames, options *Options, fopt ...v1alpha1testing.ServiceOption) *v1alpha1.Service {
	a := append([]v1alpha1testing.ServiceOption{
		v1alpha1testing.WithRunLatestConfigSpec(*LegacyConfigurationSpec(ptest.ImagePath(names.Image), options)),
	}, fopt...)
	return v1alpha1testing.ServiceWithoutNamespace(names.Service, a...)
}

// AppendRandomString will generate a random string that begins with prefix. This is useful
// if you want to make sure that your tests can run at the same time against the same
// environment without conflicting. This method will seed rand with the current time when
// called for the first time.
var AppendRandomString = helpers.AppendRandomString

// ObjectNameForTest generates a random object name based on the test name.
func ObjectNameForTest(t *testing.T) string {
	return AppendRandomString(makeK8sNamePrefix(strings.TrimPrefix(t.Name(), testNamePrefix)))
}

// SubServiceNameForTest generates a random service name based on the test name and
// the given subservice name.
func SubServiceNameForTest(t *testing.T, subsvc string) string {
	fullPrefix := strings.TrimPrefix(t.Name(), testNamePrefix) + "-" + subsvc
	return AppendRandomString(makeK8sNamePrefix(fullPrefix))
}

// makeK8sNamePrefix converts each chunk of non-alphanumeric character into a single dash
// and also convert camelcase tokens into dash-delimited lowercase tokens.
func makeK8sNamePrefix(s string) string {
	var sb strings.Builder
	newToken := false
	for _, c := range s {
		if !(unicode.IsLetter(c) || unicode.IsNumber(c)) {
			newToken = true
			continue
		}
		if sb.Len() > 0 && (newToken || unicode.IsUpper(c)) {
			sb.WriteRune('-')
		}
		sb.WriteRune(unicode.ToLower(c))
		newToken = false
	}
	return sb.String()
}
