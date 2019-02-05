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

package activator

import (
	"errors"
	"testing"

	. "github.com/knative/pkg/logging/testing"
	testinghelper "github.com/knative/serving/pkg/activator/testing"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	v1alpha12 "github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/queue"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"go.uber.org/zap"
)

var (
	revID = RevisionID{"good-namespace", "good-name"}

	sampleError = "some error"

	existingRevisionGetter = func(concurrency v1alpha1.RevisionContainerConcurrencyType) func(RevisionID) (*v1alpha12.Revision, error) {
		return func(RevisionID) (*v1alpha12.Revision, error) {
			revision := &v1alpha12.Revision{Spec: v1alpha12.RevisionSpec{ContainerConcurrency: concurrency}}
			return revision, nil
		}
	}
	nonExistingRevisionGetter = func(RevisionID) (*v1alpha12.Revision, error) {
		revision := &v1alpha12.Revision{}
		return revision, errors.New(sampleError)
	}
	initCapacity            = int32(0)
	existingEndpointsGetter = func(RevisionID) (int32, error) {
		return initCapacity, nil
	}
	nonExistingEndpointsGetter = func(RevisionID) (int32, error) {
		return initCapacity, errors.New(sampleError)
	}
)

const (
	defaultMaxConcurrency = int32(10)
)

func TestThrottler_UpdateCapacity(t *testing.T) {
	samples := []struct {
		label           string
		revisionGetter  func(RevisionID) (*v1alpha12.Revision, error)
		endpointsGetter func(RevisionID) (int32, error)
		maxConcurrency  int32
		want            int32
		wantError       string
	}{{
		label:           "all good",
		revisionGetter:  existingRevisionGetter(10),
		endpointsGetter: existingEndpointsGetter,
		maxConcurrency:  defaultMaxConcurrency,
		want:            int32(10),
	}, {
		label:           "unlimited concurrency",
		revisionGetter:  existingRevisionGetter(0),
		endpointsGetter: existingEndpointsGetter,
		maxConcurrency:  100,
		want:            int32(100),
	}, {
		label:           "non-existing revision",
		revisionGetter:  nonExistingRevisionGetter,
		endpointsGetter: existingEndpointsGetter,
		maxConcurrency:  defaultMaxConcurrency,
		want:            int32(0),
		wantError:       sampleError,
	}}
	for _, s := range samples {
		t.Run(s.label, func(t *testing.T) {
			want := s.want
			throttler := getThrottler(s.maxConcurrency, s.revisionGetter, s.endpointsGetter, TestLogger(t), initCapacity)
			err := throttler.UpdateCapacity(revID, 1)
			if s.wantError != "" {
				received := err.Error()
				if received != s.wantError {
					t.Errorf("Expected error in Update capacity. Want %s, got %s", s.wantError, err.Error())
				}
			}
			if want > 0 {
				breaker, _ := throttler.breakers[revID]
				got := breaker.Capacity()
				if got != want {
					t.Errorf("Unexpected capacity of the breaker. Want %d, got %d", want, got)
				}
			}
		})
	}
}

func TestThrottler_Try(t *testing.T) {
	samples := []struct {
		label           string
		addCapacity     bool
		wantBreakers    int32
		wantError       string
		revisionGetter  func(RevisionID) (*v1alpha12.Revision, error)
		endpointsGetter func(RevisionID) (int32, error)
	}{{
		label:           "all good",
		addCapacity:     true,
		wantBreakers:    int32(1),
		wantError:       "",
		revisionGetter:  existingRevisionGetter(10),
		endpointsGetter: existingEndpointsGetter,
	}, {
		label:           "non-existing revision",
		addCapacity:     true,
		wantBreakers:    int32(0),
		wantError:       sampleError,
		revisionGetter:  nonExistingRevisionGetter,
		endpointsGetter: existingEndpointsGetter,
	}, {
		label:           "non-existing endpoint",
		addCapacity:     false,
		wantBreakers:    int32(0),
		wantError:       sampleError,
		revisionGetter:  existingRevisionGetter(10),
		endpointsGetter: nonExistingEndpointsGetter,
	}}
	for _, s := range samples {
		t.Run(s.label, func(t *testing.T) {
			var got int32
			want := s.wantBreakers
			throttler := getThrottler(defaultMaxConcurrency, s.revisionGetter, s.endpointsGetter, TestLogger(t), initCapacity)
			if s.addCapacity {
				throttler.UpdateCapacity(revID, 1)
			}
			err := throttler.Try(revID, func() {
				got++
			})
			if s.wantError != "" {
				received := err.Error()
				if received != s.wantError {
					t.Errorf("Expected error in the Try. Want %s, got %s", s.wantError, received)
				}
			}
			if got != want {
				t.Errorf("Unexpected number of function runs in Try. Want %d, got %d", want, got)
			}
		})
	}
}

func TestUpdateEndpoints(t *testing.T) {
	samples := []struct {
		label          string
		concurrency    int32
		endpointBefore int
		endpointsAfter int
		wantCapacity   int32
		initCapacity   int32
	}{{
		label:          "delta > 0, add single endpoint",
		concurrency:    defaultMaxConcurrency,
		endpointBefore: 0,
		endpointsAfter: 1,
		wantCapacity:   10,
		initCapacity:   0,
	}, {
		label:          "delta > 0, add several endpoints",
		concurrency:    20,
		endpointBefore: 0,
		endpointsAfter: 2,
		wantCapacity:   20,
		initCapacity:   0,
	}, {
		label:          "delta = 0",
		concurrency:    10,
		endpointBefore: 1,
		endpointsAfter: 1,
		wantCapacity:   10,
		initCapacity:   10,
	}, {
		label:          "delta < 0",
		concurrency:    10,
		endpointBefore: 2,
		endpointsAfter: 1,
		wantCapacity:   10,
		initCapacity:   10,
	}}

	for _, s := range samples {
		throttler := getThrottler(s.concurrency, existingRevisionGetter(10), existingEndpointsGetter, TestLogger(t), s.initCapacity)
		throttler.breakers[revID] = queue.NewBreaker(throttler.breakerParams)
		updater := UpdateEndpoints(throttler)

		endpointsBefore := corev1.Endpoints{ObjectMeta: v1.ObjectMeta{Name: revID.Name + "-service", Namespace: revID.Namespace}, Subsets: testinghelper.GetTestEndpointsSubset(s.endpointBefore, 1)}
		endpointsAfter := corev1.Endpoints{ObjectMeta: v1.ObjectMeta{Name: revID.Name + "-service", Namespace: revID.Namespace}, Subsets: testinghelper.GetTestEndpointsSubset(s.endpointsAfter, 1)}
		updater(&endpointsBefore, &endpointsAfter)

		breaker, _ := throttler.breakers[revID]
		got := breaker.Capacity()
		if got != s.wantCapacity {
			t.Errorf("Unexpected Breaker capacity received. Want %d, got %d", s.wantCapacity, got)
		}
	}
}

func TestThrottler_Remove(t *testing.T) {
	throttler := getThrottler(defaultMaxConcurrency, existingRevisionGetter(10), existingEndpointsGetter, TestLogger(t), initCapacity)
	throttler.breakers[revID] = queue.NewBreaker(throttler.breakerParams)
	got := len(throttler.breakers)
	if got != 1 {
		t.Errorf("Unexpected number of Breakers was created. Want %d, got %d", 1, got)
	}
	throttler.Remove(revID)
	got = len(throttler.breakers)
	if got != 0 {
		t.Errorf("Unexpected number of Breakers was created. Want %d, got %d", 0, got)
	}
}

func TestHelper_DeleteBreaker(t *testing.T) {
	throttler := getThrottler(int32(20), existingRevisionGetter(10), existingEndpointsGetter, TestLogger(t), initCapacity)
	revision := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      revID.Name,
			Namespace: revID.Namespace,
		},
	}
	revID := RevisionID{Namespace: revID.Namespace, Name: revID.Name}
	throttler.breakers[revID] = queue.NewBreaker(throttler.breakerParams)
	if len(throttler.breakers) != 1 {
		t.Errorf("Breaker map size didn't change. Wanted %d, got %d", 1, len(throttler.breakers))
	}
	deleter := DeleteBreaker(throttler)
	deleter(revision)
	if len(throttler.breakers) != 0 {
		t.Error("Breaker map is not empty")
	}
}

func getThrottler(maxConcurrency int32, revisionGetter func(RevisionID) (*v1alpha12.Revision, error), endpointsGetter func(RevisionID) (int32, error), logger *zap.SugaredLogger, initCapacity int32) *Throttler {
	params := queue.BreakerParams{QueueDepth: 10, MaxConcurrency: maxConcurrency, InitialCapacity: initCapacity}
	throttlerParams := ThrottlerParams{BreakerParams: params, Logger: logger, GetRevision: revisionGetter, GetEndpoints: endpointsGetter}
	return NewThrottler(throttlerParams)
}
