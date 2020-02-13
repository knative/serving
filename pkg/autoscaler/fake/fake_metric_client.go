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

package fake

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubeinformers "k8s.io/client-go/informers"
	fakek8s "k8s.io/client-go/kubernetes/fake"
)

var (
	// KubeClient holds instances of interfaces for making requests to kubernetes client.
	KubeClient = fakek8s.NewSimpleClientset()
	// KubeInformer constructs a new instance of sharedInformerFactory for all namespaces.
	KubeInformer = kubeinformers.NewSharedInformerFactory(KubeClient, 0)
)

const (
	// TestRevision is the name used for the revision.
	TestRevision = "test-revision"
	// TestService is the name used for the service.
	TestService = "test-revision-metrics"
	// TestNamespace is the name used for the namespace.
	TestNamespace = "test-namespace"
)

// MetricClient is a fake implementation of autoscaler.MetricClient for testing.
type MetricClient struct {
	StableConcurrency float64
	PanicConcurrency  float64
	StableRPS         float64
	PanicRPS          float64
	ErrF              func(key types.NamespacedName, now time.Time) error
	RemovalCandidates []string
}

// A ManualTickProvider holds a channel that delivers `ticks' of a clock at intervals.
type ManualTickProvider struct {
	Channel chan time.Time
}

// NewTicker returns a Ticker containing a channel that will send the
// time with a period specified by the duration argument.
func (mtp *ManualTickProvider) NewTicker(time.Duration) *time.Ticker {
	return &time.Ticker{
		C: mtp.Channel,
	}
}

// StableAndPanicConcurrency returns stable/panic concurrency stored in the object
// and the result of Errf as the error.
func (t *MetricClient) StableAndPanicConcurrency(key types.NamespacedName, now time.Time) (float64, float64, error) {
	var err error
	if t.ErrF != nil {
		err = t.ErrF(key, now)
	}
	return t.StableConcurrency, t.PanicConcurrency, err
}

// StableAndPanicRPS returns stable/panic RPS stored in the object
// and the result of Errf as the error.
func (t *MetricClient) StableAndPanicRPS(key types.NamespacedName, now time.Time) (float64, float64, error) {
	var err error
	if t.ErrF != nil {
		err = t.ErrF(key, now)
	}
	return t.StableRPS, t.PanicRPS, err
}

// CandidatesForRemoval returns names of pods which are candidate for removal
func (t *MetricClient) CandidatesForRemoval(key types.NamespacedName, readyCount, desiredScale int) ([]string, error) {
	return t.RemovalCandidates, nil
}

// StaticMetricClient returns stable/panic concurrency and RPS with static value, i.e. 10.
var StaticMetricClient = MetricClient{
	StableConcurrency: 10.0,
	PanicConcurrency:  10.0,
	StableRPS:         10.0,
	PanicRPS:          10.0,
}

// Endpoints is used to create endpoints.
func Endpoints(count int, svc string) {
	epAddresses := make([]corev1.EndpointAddress, count)
	for i := 1; i <= count; i++ {
		ip := fmt.Sprintf("127.0.0.%v", i)
		epAddresses[i-1] = corev1.EndpointAddress{IP: ip}
	}

	ep := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: TestNamespace,
			Name:      svc,
		},
		Subsets: []corev1.EndpointSubset{{
			Addresses: epAddresses,
		}},
	}
	KubeClient.CoreV1().Endpoints(TestNamespace).Create(ep)
	KubeInformer.Core().V1().Endpoints().Informer().GetIndexer().Add(ep)
}
