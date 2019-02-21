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
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	revID = RevisionID{"good-namespace", "good-name"}

	existingRevisionGetter = func(concurrency int) func(RevisionID) (*v1alpha12.Revision, error) {
		return func(RevisionID) (*v1alpha12.Revision, error) {
			return &v1alpha12.Revision{Spec: v1alpha12.RevisionSpec{ContainerConcurrency: v1alpha1.RevisionContainerConcurrencyType(concurrency)}}, nil
		}
	}
	nonExistingRevisionGetter = func(RevisionID) (*v1alpha12.Revision, error) {
		return nil, errors.New(sampleError)
	}

	existingEndpointsGetter = func(count int) func(RevisionID) (int32, error) {
		return func(RevisionID) (int32, error) {
			return int32(count), nil
		}
	}
	nonExistingEndpointsGetter = func(RevisionID) (int32, error) {
		return initCapacity, errors.New(sampleError)
	}
)

const (
	defaultMaxConcurrency = int32(1000)
	initCapacity          = int32(0)
	sampleError           = "some error"
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
		endpointsGetter: existingEndpointsGetter(0),
		maxConcurrency:  defaultMaxConcurrency,
		want:            int32(10),
	}, {
		label:           "unlimited concurrency",
		revisionGetter:  existingRevisionGetter(0),
		endpointsGetter: existingEndpointsGetter(0),
		maxConcurrency:  100,
		want:            int32(100),
	}, {
		label:           "non-existing revision",
		revisionGetter:  nonExistingRevisionGetter,
		endpointsGetter: existingEndpointsGetter(0),
		maxConcurrency:  defaultMaxConcurrency,
		want:            int32(0),
		wantError:       sampleError,
	}, {
		label:           "exceeds maxConcurrency",
		revisionGetter:  existingRevisionGetter(10),
		endpointsGetter: existingEndpointsGetter(0),
		maxConcurrency:  int32(5),
		want:            int32(5),
	}}
	for _, s := range samples {
		t.Run(s.label, func(t *testing.T) {
			throttler := getThrottler(s.maxConcurrency, s.revisionGetter, s.endpointsGetter, TestLogger(t), initCapacity)
			err := throttler.UpdateCapacity(revID, 1)
			if s.wantError != "" {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				if got := err.Error(); got != s.wantError {
					t.Errorf("Update capacity error message = %s, want: %s", got, s.wantError)
				}
			}
			if s.want > 0 {
				breaker, _ := throttler.breakers[revID]
				if got := breaker.Capacity(); got != s.want {
					t.Errorf("Breaker Capacity = %d, want: %d", got, s.want)
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
		revisionGetter:  existingRevisionGetter(10),
		endpointsGetter: existingEndpointsGetter(0),
	}, {
		label:           "non-existing revision",
		addCapacity:     true,
		wantBreakers:    int32(0),
		wantError:       sampleError,
		revisionGetter:  nonExistingRevisionGetter,
		endpointsGetter: existingEndpointsGetter(0),
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
			throttler := getThrottler(
				defaultMaxConcurrency, s.revisionGetter, s.endpointsGetter, TestLogger(t), initCapacity)
			if s.addCapacity {
				throttler.UpdateCapacity(revID, 1)
			}
			err := throttler.Try(revID, func() {
				got++
			})
			if s.wantError != "" {
				if err == nil {
					t.Fatal("Expected error got nil")
				}

				if got := err.Error(); got != s.wantError {
					t.Errorf("Try error = %s, want: %s", got, s.wantError)
				}
			}
			if got != want {
				t.Errorf("Unexpected number of function runs in Try = %d, want: %d", got, want)
			}
		})
	}
}

func TestThrottler_TryOverload(t *testing.T) {
	maxConcurrency := 1
	initialCapacity := 1
	queueLength := 1
	th := getThrottler(int32(maxConcurrency), existingRevisionGetter(1), existingEndpointsGetter(1), TestLogger(t), int32(initialCapacity))

	doneCh := make(chan struct{})
	errCh := make(chan error)

	// Make one more request than allowed.
	allowedRequests := initialCapacity + queueLength
	for i := 0; i < allowedRequests+1; i++ {
		go func() {
			err := th.Try(revID, func() {
				doneCh <- struct{}{} // Blocks forever
			})
			if err != nil {
				errCh <- err
			}
		}()
	}

	if err := <-errCh; err != ErrActivatorOverload {
		t.Errorf("error = %v, want: %v", err, ErrActivatorOverload)
	}

	successfulRequests := 0
	for i := 0; i < allowedRequests; i++ {
		select {
		case <-doneCh:
			successfulRequests++
		case <-errCh:
			t.Errorf("Only one request should fail.")
		}
	}

	if successfulRequests != allowedRequests {
		t.Errorf("successfulRequests = %d, want: %d", successfulRequests, allowedRequests)
	}
}

func TestUpdateEndpoints(t *testing.T) {
	revisionConcurrency := 10

	samples := []struct {
		label          string
		endpointsAfter int
		wantCapacity   int
		initCapacity   int
	}{{
		label:          "add single endpoint",
		endpointsAfter: 1,
		wantCapacity:   1 * revisionConcurrency,
	}, {
		label:          "add several endpoints",
		endpointsAfter: 2,
		wantCapacity:   2 * revisionConcurrency,
	}, {
		label:          "do nothing",
		endpointsAfter: 1,
		wantCapacity:   1 * revisionConcurrency,
		initCapacity:   10,
	}, {
		label:          "reduce endpoints",
		endpointsAfter: 0,
		wantCapacity:   0,
		initCapacity:   10,
	}, {
		label:          "exceed max concurrency",
		endpointsAfter: 101 * revisionConcurrency,
		wantCapacity:   int(defaultMaxConcurrency),
	}}

	for _, s := range samples {
		t.Run(s.label, func(t *testing.T) {
			throttler := getThrottler(defaultMaxConcurrency, existingRevisionGetter(revisionConcurrency), existingEndpointsGetter(0), TestLogger(t), int32(s.initCapacity))
			breaker := queue.NewBreaker(throttler.breakerParams)
			throttler.breakers[revID] = breaker
			updater := UpdateEndpoints(throttler)
			endpointsAfter := corev1.Endpoints{ObjectMeta: metav1.ObjectMeta{Name: revID.Name + "-service", Namespace: revID.Namespace}, Subsets: testinghelper.GetTestEndpointsSubset(s.endpointsAfter, 1)}
			updater(&endpointsAfter)

			if got := breaker.Capacity(); got != int32(s.wantCapacity) {
				t.Errorf("Breaker capacity = %d, want: %d", got, s.wantCapacity)
			}
		})
	}
}

func TestThrottler_Remove(t *testing.T) {
	throttler := getThrottler(defaultMaxConcurrency, existingRevisionGetter(10), existingEndpointsGetter(0), TestLogger(t), initCapacity)
	throttler.breakers[revID] = queue.NewBreaker(throttler.breakerParams)

	if got := len(throttler.breakers); got != 1 {
		t.Errorf("Number of Breakers created = %d, want: 1", got)
	}
	throttler.Remove(revID)

	if got := len(throttler.breakers); got != 0 {
		t.Errorf("Number of Breakers created = %d, want: %d", got, 0)
	}
}

func TestHelper_DeleteBreaker(t *testing.T) {
	throttler := getThrottler(int32(20), existingRevisionGetter(10), existingEndpointsGetter(0), TestLogger(t), initCapacity)
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      revID.Name,
			Namespace: revID.Namespace,
		},
	}
	revID := RevisionID{Namespace: revID.Namespace, Name: revID.Name}
	throttler.breakers[revID] = queue.NewBreaker(throttler.breakerParams)
	if got := len(throttler.breakers); got != 1 {
		t.Errorf("Breaker map size got %d, want: 1", got)
	}
	DeleteBreaker(throttler)(endpoints)
	if len(throttler.breakers) != 0 {
		t.Error("Breaker map is not empty")
	}
}

func getThrottler(
	maxConcurrency int32, revisionGetter func(RevisionID) (*v1alpha12.Revision, error),
	endpointsGetter func(RevisionID) (int32, error), logger *zap.SugaredLogger,
	initCapacity int32) *Throttler {
	params := queue.BreakerParams{QueueDepth: 1, MaxConcurrency: maxConcurrency, InitialCapacity: initCapacity}
	throttlerParams := ThrottlerParams{BreakerParams: params, Logger: logger, GetRevision: revisionGetter, GetEndpoints: endpointsGetter}
	return NewThrottler(throttlerParams)
}
