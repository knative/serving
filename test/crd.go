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
	"math/rand"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

// ResourceNames holds names of various resources.
type ResourceNames struct {
	Config        string
	Route         string
	Revision      string
	Service       string
	TrafficTarget string
}

// ResourceObjects holds types of the resource objects.
type ResourceObjects struct {
	Route         *v1alpha1.Route
	Configuration *v1alpha1.Configuration
	Service       *v1alpha1.Service
}

// Route returns a Route object in namespace using the route and configuration
// names in names.
func Route(namespace string, names ResourceNames) *v1alpha1.Route {
	return &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      names.Route,
		},
		Spec: v1alpha1.RouteSpec{
			Traffic: []v1alpha1.TrafficTarget{
				{
					Name:              names.TrafficTarget,
					ConfigurationName: names.Config,
					Percent:           100,
				},
			},
		},
	}
}

// BlueGreenRoute returns a Route object in namespace using the route and configuration
// names in names. Traffic is split evenly between blue and green.
func BlueGreenRoute(namespace string, names, blue, green ResourceNames) *v1alpha1.Route {
	return &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      names.Route,
		},
		Spec: v1alpha1.RouteSpec{
			Traffic: []v1alpha1.TrafficTarget{{
				Name:         blue.TrafficTarget,
				RevisionName: blue.Revision,
				Percent:      50,
			}, {
				Name:         green.TrafficTarget,
				RevisionName: green.Revision,
				Percent:      50,
			}},
		},
	}
}

// Configuration returns a Configuration object in namespace with the name names.Config
// that uses the image specified by imagePath.
func Configuration(namespace string, names ResourceNames, imagePath string, options *Options) *v1alpha1.Configuration {
	config := &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      names.Config,
		},
		Spec: v1alpha1.ConfigurationSpec{
			RevisionTemplate: v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					Container: corev1.Container{
						Image: imagePath,
					},
					ContainerConcurrency: v1alpha1.RevisionContainerConcurrencyType(options.ContainerConcurrency),
				},
			},
		},
	}
	if options.EnvVars != nil && len(options.EnvVars) > 0 {
		config.Spec.RevisionTemplate.Spec.Container.Env = options.EnvVars
	}
	return config
}

func ConfigurationWithBuild(namespace string, names ResourceNames, build *v1alpha1.RawExtension, imagePath string) *v1alpha1.Configuration {
	return &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      names.Config,
		},
		Spec: v1alpha1.ConfigurationSpec{
			Build: build,
			RevisionTemplate: v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					Container: corev1.Container{
						Image: imagePath,
					},
				},
			},
		},
	}
}

// LatestService returns a RunLatest Service object in namespace with the name names.Service
// that uses the image specified by imagePath.
func LatestService(namespace string, names ResourceNames, imagePath string) *v1alpha1.Service {
	return &v1alpha1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      names.Service,
		},
		Spec: v1alpha1.ServiceSpec{
			RunLatest: &v1alpha1.RunLatestType{
				Configuration: v1alpha1.ConfigurationSpec{
					RevisionTemplate: v1alpha1.RevisionTemplateSpec{
						Spec: v1alpha1.RevisionSpec{
							Container: corev1.Container{
								Image: imagePath,
							},
						},
					},
				},
			},
		},
	}
}

// ReleaseService returns a Release Service object in namespace with the name names.Service that uses
// the image specifeid by imagePath. It also takes a list of 1-2 revisons and a rolloutPercent to be
// used to configure routing
func ReleaseService(svc *v1alpha1.Service, revisions []string, rolloutPercent int) *v1alpha1.Service {
	var config v1alpha1.ConfigurationSpec
	if svc.Spec.RunLatest != nil {
		config = svc.Spec.RunLatest.Configuration
	} else if svc.Spec.Release != nil {
		config = svc.Spec.Release.Configuration
	} else if svc.Spec.Pinned != nil {
		config = svc.Spec.Pinned.Configuration
	}
	return &v1alpha1.Service{
		ObjectMeta: svc.ObjectMeta,
		Spec: v1alpha1.ServiceSpec{
			Release: &v1alpha1.ReleaseType{
				Revisions:      revisions,
				RolloutPercent: rolloutPercent,
				Configuration:  config,
			},
		},
	}
}

// ManualService returns a Manual Service object in namespace with the name names.Service
func ManualService(svc *v1alpha1.Service) *v1alpha1.Service {
	return &v1alpha1.Service{
		ObjectMeta: svc.ObjectMeta,
		Spec: v1alpha1.ServiceSpec{
			Manual: &v1alpha1.ManualType{},
		},
	}
}

const (
	letterBytes   = "abcdefghijklmnopqrstuvwxyz"
	randSuffixLen = 8
)

// r is used by AppendRandomString to generate a random string. It is seeded with the time
// at import so the strings will be different between test runs.
var (
	r        *rand.Rand
	rndMutex *sync.Mutex
)

// once is used to initialize r
var once sync.Once

func initSeed(logger *logging.BaseLogger) func() {
	return func() {
		seed := time.Now().UTC().UnixNano()
		logger.Infof("Seeding rand.Rand with %v", seed)
		r = rand.New(rand.NewSource(seed))
		rndMutex = &sync.Mutex{}
	}
}

// AppendRandomString will generate a random string that begins with prefix. This is useful
// if you want to make sure that your tests can run at the same time against the same
// environment without conflicting. This method will seed rand with the current time when
// called for the first time.
func AppendRandomString(prefix string, logger *logging.BaseLogger) string {
	once.Do(initSeed(logger))
	suffix := make([]byte, randSuffixLen)
	rndMutex.Lock()
	for i := range suffix {
		suffix[i] = letterBytes[r.Intn(len(letterBytes))]
	}
	rndMutex.Unlock()
	return prefix + string(suffix)
}
