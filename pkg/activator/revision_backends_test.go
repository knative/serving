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
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"knative.dev/pkg/controller"
	. "knative.dev/pkg/logging/testing"
	activatortest "knative.dev/serving/pkg/activator/testing"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/queue"
)

func TestEndpointsToDests(t *testing.T) {
	for _, tc := range []struct {
		name        string
		endpoints   corev1.Endpoints
		expectDests []string
	}{{
		name:        "no endpoints",
		endpoints:   corev1.Endpoints{},
		expectDests: []string{},
	}, {
		name: "single endpoint single address",
		endpoints: corev1.Endpoints{
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{
					IP: "128.0.0.1",
				}},
				Ports: []corev1.EndpointPort{{
					Name: networking.ServicePortNameHTTP1,
					Port: 1234,
				}},
			}},
		},
		expectDests: []string{"128.0.0.1:1234"},
	}, {
		name: "single endpoint multiple address",
		endpoints: corev1.Endpoints{
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{
					IP: "128.0.0.1",
				}, {
					IP: "128.0.0.2",
				}},
				Ports: []corev1.EndpointPort{{
					Name: networking.ServicePortNameHTTP1,
					Port: 1234,
				}},
			}},
		},
		expectDests: []string{"128.0.0.1:1234", "128.0.0.2:1234"},
	}, {
		name: "multiple endpoint filter port",
		endpoints: corev1.Endpoints{
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{
					IP: "128.0.0.1",
				}},
				Ports: []corev1.EndpointPort{{
					Name: networking.ServicePortNameHTTP1,
					Port: 1234,
				}},
			}, {
				Addresses: []corev1.EndpointAddress{{
					IP: "128.0.0.2",
				}},
				Ports: []corev1.EndpointPort{{
					Name: "other-protocol",
					Port: 1234,
				}},
			}},
		},
		expectDests: []string{"128.0.0.1:1234"},
	}} {

		t.Run(tc.name, func(t *testing.T) {
			dests := endpointsToDests(&tc.endpoints)

			if diff := cmp.Diff(tc.expectDests, dests); diff != "" {
				t.Errorf("Got unexpected dests (-want, +got): %v", diff)
			}
		})

	}
}

func TestRevisionWatcher(t *testing.T) {
	for _, tc := range []struct {
		name           string
		dests          []string
		expectHealthy  []string
		probeResponses []activatortest.FakeResponse
		ticks          []time.Time
		updateCnt      int
	}{{
		name:          "single healthy dest",
		dests:         []string{"128.0.0.1:1234"},
		expectHealthy: []string{"128.0.0.1:1234"},
		probeResponses: []activatortest.FakeResponse{{
			Err:  nil,
			Code: http.StatusOK,
			Body: queue.Name,
		}},
	}, {
		name:          "single unavailable dest",
		dests:         []string{"128.0.0.1:1234"},
		expectHealthy: []string{},
		probeResponses: []activatortest.FakeResponse{{
			Err:  nil,
			Code: http.StatusServiceUnavailable,
			Body: queue.Name,
		}},
	}, {
		name:          "single error dest",
		dests:         []string{"128.0.0.1:1234"},
		expectHealthy: []string{},
		probeResponses: []activatortest.FakeResponse{{
			Err:  errors.New("Fake error"),
			Code: http.StatusOK,
			Body: queue.Name,
		}},
	}, {
		name:          "dest slow ready",
		dests:         []string{"128.0.0.1:1234"},
		expectHealthy: []string{"128.0.0.1:1234"},
		probeResponses: []activatortest.FakeResponse{{
			Err:  nil,
			Code: http.StatusServiceUnavailable,
			Body: queue.Name,
		}, {
			Err:  nil,
			Code: http.StatusOK,
			Body: queue.Name,
		}},
		ticks:     []time.Time{time.Now()},
		updateCnt: 2,
	}, {
		name:          "multiple healthy dest",
		dests:         []string{"128.0.0.1:1234", "128.0.0.2:1234"},
		expectHealthy: []string{"128.0.0.1:1234", "128.0.0.2:1234"},
		probeResponses: []activatortest.FakeResponse{{
			Err:  nil,
			Code: http.StatusOK,
			Body: queue.Name,
		}},
	}} {

		t.Run(tc.name, func(t *testing.T) {
			fakeRt := activatortest.FakeRoundTripper{
				ExpectHost:     "test-revision",
				ProbeResponses: tc.probeResponses,
			}
			rt := network.RoundTripperFunc(fakeRt.RT)

			updateCh := make(chan *RevisionDestsUpdate, 100)
			defer close(updateCh)

			tickerCh := make(chan time.Time, 1)
			defer close(tickerCh)

			// This gets cleaned up as part of the test
			destsCh := make(chan []string)

			if tc.updateCnt == 0 {
				tc.updateCnt = 1
			}

			revID := RevisionID{Namespace: "test-namespace", Name: "test-revision"}
			rw := newRevisionWatcher(
				revID,
				updateCh,
				destsCh,
				rt,
				TestLogger(t),
			)

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				rw.runWithTickCh(tickerCh)
				wg.Done()
			}()

			destsCh <- tc.dests

			for _, tick := range tc.ticks {
				tickerCh <- tick
			}

			revDests := make(map[RevisionID][]string)
			for i := 0; i < tc.updateCnt; i++ {
				select {
				case update := <-updateCh:
					sort.Strings(update.Dests)
					revDests[update.Rev] = update.Dests
				case <-time.After(100 * time.Millisecond):
					t.Errorf("Timed out waiting for update event")
				}
			}

			// Shutdown run loop
			close(destsCh)

			wg.Wait()

			expectDests := map[RevisionID][]string{
				revID: tc.expectHealthy,
			}

			if diff := cmp.Diff(expectDests, revDests); diff != "" {
				t.Errorf("Got unexpected revision dests (-want, +got): %v", diff)
			}
		})

	}
}

func TestRevisionBackendManagerAddEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name           string
		endpointsArr   []corev1.Endpoints
		probeResponses []activatortest.FakeResponse
		expectDests    map[RevisionID][]string
		updateCnt      int
	}{{
		name: "Add slow healthy",
		endpointsArr: []corev1.Endpoints{{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					serving.RevisionUID:       "test",
					networking.ServiceTypeKey: string(networking.ServiceTypePrivate),
					serving.RevisionLabelKey:  "test-revision",
				},
			},
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{
					IP: "128.0.0.1",
				}},
				Ports: []corev1.EndpointPort{{
					Name: networking.ServicePortNameHTTP1,
					Port: 1234,
				}},
			}},
		}},
		probeResponses: []activatortest.FakeResponse{{
			Err:  nil,
			Code: http.StatusServiceUnavailable,
			Body: queue.Name,
		}, {
			Err:  nil,
			Code: http.StatusOK,
			Body: queue.Name,
		}},
		expectDests: map[RevisionID][]string{
			{Namespace: "test-namespace", Name: "test-revision"}: {"128.0.0.1:1234"},
		},
		updateCnt: 2,
	}, {
		name: "Multiple revisions",
		endpointsArr: []corev1.Endpoints{{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-ep-1",
				Labels: map[string]string{
					serving.RevisionUID:       "test1",
					networking.ServiceTypeKey: string(networking.ServiceTypePrivate),
					serving.RevisionLabelKey:  "test-revision1",
				},
			},
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{
					IP: "128.0.0.1",
				}},
				Ports: []corev1.EndpointPort{{
					Name: networking.ServicePortNameHTTP1,
					Port: 1234,
				}},
			}},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-ep-2",
				Labels: map[string]string{
					serving.RevisionUID:       "test2",
					networking.ServiceTypeKey: string(networking.ServiceTypePrivate),
					serving.RevisionLabelKey:  "test-revision2",
				},
			},
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{
					IP: "128.0.0.2",
				}},
				Ports: []corev1.EndpointPort{{
					Name: networking.ServicePortNameHTTP1,
					Port: 1234,
				}},
			}},
		}},
		probeResponses: []activatortest.FakeResponse{{
			Err:  nil,
			Code: http.StatusOK,
			Body: queue.Name,
		}},
		expectDests: map[RevisionID][]string{
			{Namespace: "test-namespace", Name: "test-revision1"}: {"128.0.0.1:1234"},
			{Namespace: "test-namespace", Name: "test-revision2"}: {"128.0.0.2:1234"},
		},
		updateCnt: 2,
	}} {

		t.Run(tc.name, func(t *testing.T) {
			fakeRt := activatortest.FakeRoundTripper{
				ExpectHost:     "test-revision",
				ProbeResponses: tc.probeResponses,
			}
			rt := network.RoundTripperFunc(fakeRt.RT)

			fake := kubefake.NewSimpleClientset()
			informer := kubeinformers.NewSharedInformerFactory(fake, 0)
			endpointsInformer := informer.Core().V1().Endpoints()

			stopCh := make(chan struct{})
			defer close(stopCh)
			controller.StartInformers(stopCh, endpointsInformer.Informer())

			updateCh := make(chan *RevisionDestsUpdate, 100)
			defer close(updateCh)

			bm := NewRevisionBackendsManagerWithProbeFrequency(updateCh, rt, endpointsInformer, TestLogger(t), 50*time.Millisecond)
			defer bm.Clear()

			for _, ep := range tc.endpointsArr {
				fake.CoreV1().Endpoints("test-namespace").Create(&ep)
				endpointsInformer.Informer().GetIndexer().Add(&ep)
			}

			if tc.updateCnt == 0 {
				tc.updateCnt = 1
			}

			revDests := make(map[RevisionID][]string)
			// Wait for updateCb to be called
			for i := 0; i < tc.updateCnt; i++ {
				select {
				case update := <-updateCh:
					sort.Strings(update.Dests)
					revDests[update.Rev] = update.Dests
				case <-time.After(100 * time.Millisecond):
					t.Errorf("Timed out waiting for update event")
				}
			}

			if diff := cmp.Diff(tc.expectDests, revDests); diff != "" {
				t.Errorf("Got unexpected revision dests (-want, +got): %v", diff)
			}
		})

	}
}
