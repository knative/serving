/*
Copyright 2018 Google Inc. All Rights Reserved.
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
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ResourceNames holds names of related Config, Route and Revision objects.
type ResourceNames struct {
	Config   string
	Route    string
	Revision string
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
				v1alpha1.TrafficTarget{
					Name:              names.Route,
					ConfigurationName: names.Config,
					Percent:           100,
				},
			},
		},
	}
}

// Configuration returns a Configuration object in namespace with the name names.Config
// that uses the image specifed by imagePath.
func Configuration(namespace string, names ResourceNames, imagePath string) *v1alpha1.Configuration {
	return &v1alpha1.Configuration{
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
				},
			},
		},
	}
}

const (
	letterBytes   = "abcdefghijklmnopqrstuvwxyz"
	randSuffixLen = 8
)

// r is used by AppendRandomString to generate a random string. It is seeded with the time
// at import so the strings will be different between test runs.
var r *rand.Rand

// once is used to initialize r
var once sync.Once

func initSeed() {
	seed := time.Now().UTC().UnixNano()
	log.Printf("Seeding rand.Rand with %v\n", seed)
	r = rand.New(rand.NewSource(seed))
}

// AppendRandomString will generate a random string that begins with prefix. This is useful
// if you want to make sure that your tests can run at the same time against the same
// environment without conflicting. This method will seed rand with the current time when
// called for the first time.
func AppendRandomString(prefix string) string {
	once.Do(initSeed)
	suffix := make([]byte, randSuffixLen)
	for i := range suffix {
		suffix[i] = letterBytes[r.Intn(len(letterBytes))]
	}
	return prefix + string(suffix)
}
