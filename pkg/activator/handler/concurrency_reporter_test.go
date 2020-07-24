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

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/metrics/metricskey"
	"knative.dev/pkg/metrics/metricstest"
	_ "knative.dev/pkg/metrics/testing"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/serving/pkg/activator/util"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/autoscaler/metrics"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
	"knative.dev/serving/pkg/network"
)

const (
	requestOpTick int = iota + 1
	requestOpStart
	requestOpEnd
)

const activatorPodName = "the-best-activator"

var (
	rev1 = types.NamespacedName{Namespace: "test", Name: "rev1"}
	rev2 = types.NamespacedName{Namespace: "test", Name: "rev2"}
	rev3 = types.NamespacedName{Namespace: "test", Name: "rev3"}
)

type reqOp struct {
	op   int
	time int
	key  types.NamespacedName
}

func TestStats(t *testing.T) {
	tt := []struct {
		name          string
		ops           []reqOp
		expectedStats []metrics.StatMessage
	}{{
		name: "scale-from-zero sends stat",
		ops: []reqOp{{
			op:  requestOpStart,
			key: rev1,
		}, {
			op:  requestOpStart,
			key: rev2,
		}},
		expectedStats: []metrics.StatMessage{{
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   activatorPodName,
			},
		}, {
			Key: rev2,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   activatorPodName,
			},
		}},
	}, {
		name: "in'n out",
		ops: []reqOp{{
			op:   requestOpStart,
			key:  rev1,
			time: 0,
		}, {
			op:   requestOpEnd,
			key:  rev1,
			time: 1,
		}, {
			op:   requestOpStart,
			key:  rev1,
			time: 1,
		}, {
			op:   requestOpEnd,
			key:  rev1,
			time: 2,
		}, {
			op:   requestOpTick,
			time: 2,
		}},
		expectedStats: []metrics.StatMessage{{
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   activatorPodName,
			},
		}, {
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 0,
				RequestCount:              1,
				PodName:                   activatorPodName,
			},
		}},
	}, {
		name: "scale to two",
		ops: []reqOp{{
			op:   requestOpStart,
			key:  rev1,
			time: 0,
		}, {
			op:   requestOpStart,
			key:  rev1,
			time: 0,
		}, {
			op:   requestOpTick,
			time: 1,
		}, {
			op:   requestOpTick,
			time: 2,
		}},
		expectedStats: []metrics.StatMessage{{
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   activatorPodName,
			},
		}, {
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1, // We subtract the one concurrent request we already reported.
				RequestCount:              1,
				PodName:                   activatorPodName,
			},
		}, {
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 2, // Next reporting period, report both requests in flight.
				RequestCount:              0, // No new requests have appeared.
				PodName:                   activatorPodName,
			},
		}},
	}, {
		name: "scale-from-zero after tick sends stat",
		ops: []reqOp{{
			op:   requestOpStart,
			key:  rev1,
			time: 0,
		}, {
			op:   requestOpEnd,
			key:  rev1,
			time: 1,
		}, {
			op:   requestOpTick, // ticks a zero stat but doesn't unset state
			time: 1,
		}, {
			op:   requestOpTick, // nothing happened, unset state
			time: 2,
		}, {
			op:   requestOpStart, // scale from 0 again
			key:  rev1,
			time: 3,
		}},
		expectedStats: []metrics.StatMessage{{
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1, // scale from 0 stat
				RequestCount:              1,
				PodName:                   activatorPodName,
			},
		}, {
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 0, // first stat, discounted by 1
				RequestCount:              0,
				PodName:                   activatorPodName,
			},
		}, {
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 0, // nothing seen for the entire period
				RequestCount:              0,
				PodName:                   activatorPodName,
			},
		}, {
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1, // scale from 0 again
				RequestCount:              1,
				PodName:                   activatorPodName,
			},
		}},
	}, {
		name: "multiple revisions tick",
		ops: []reqOp{{
			op:  requestOpStart,
			key: rev1,
		}, {
			op:  requestOpStart,
			key: rev2,
		}, {
			op:  requestOpStart,
			key: rev3,
		}, {
			op: requestOpTick,
		}},
		expectedStats: []metrics.StatMessage{{
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   activatorPodName,
			},
		}, {
			Key: rev2,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   activatorPodName,
			},
		}, {
			Key: rev3,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   activatorPodName,
			},
		}, {
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 0,
				RequestCount:              0,
				PodName:                   activatorPodName,
			},
		}, {
			Key: rev2,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 0,
				RequestCount:              0,
				PodName:                   activatorPodName,
			},
		}, {
			Key: rev3,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 0,
				RequestCount:              0,
				PodName:                   activatorPodName,
			},
		}},
	}, {
		name: "interleaved requests",
		ops: []reqOp{{
			op:   requestOpStart,
			key:  rev1,
			time: 0,
		}, {
			op:   requestOpStart,
			key:  rev1,
			time: 0,
		}, {
			op:   requestOpEnd,
			key:  rev1,
			time: 1,
		}, {
			op:   requestOpEnd,
			key:  rev1,
			time: 2,
		}, {
			op: requestOpTick,
		}},
		expectedStats: []metrics.StatMessage{{
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   activatorPodName,
			},
		}, {
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 0.5,
				RequestCount:              1,
				PodName:                   activatorPodName,
			},
		}},
	}}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			cr, _, cancel := newTestReporter(t)
			defer cancel()

			// Apply request operations
			for _, op := range tc.ops {
				time := time.Time{}.Add(time.Duration(op.time) * time.Millisecond)
				switch op.op {
				case requestOpStart:
					cr.handleEvent(network.ReqEvent{Key: op.key, Type: network.ReqIn, Time: time})
				case requestOpEnd:
					cr.handleEvent(network.ReqEvent{Key: op.key, Type: network.ReqOut, Time: time})
				case requestOpTick:
					stats := cr.report(time)
					if len(stats) > 0 {
						cr.statCh <- stats
					}
				}
			}

			// Gather reported stats.
			stats := make([]metrics.StatMessage, 0, len(tc.expectedStats))
			for len(stats) < len(tc.expectedStats) {
				select {
				case x := <-cr.statCh:
					stats = append(stats, x...)
				case <-time.After(time.Second):
					t.Fatal("Timed out waiting for the event")
				}
			}

			// Verify we're not getting extra events.
			select {
			case x := <-cr.statCh:
				t.Fatal("Extra events received:", x)
			case <-time.After(5 * time.Millisecond):
				// Lookin' good.
			}

			// We need to sort receiving stats, because there's map iteration
			// which is not order consistent.
			sort.SliceStable(stats, func(i, j int) bool {
				return stats[i].Key.Name < stats[j].Key.Name
			})
			// We need to sort test stats, since we're listing them in test in logical
			// order, but after sorting and map operations above they need to match.
			sort.SliceStable(tc.expectedStats, func(i, j int) bool {
				return tc.expectedStats[i].Key.Name < tc.expectedStats[j].Key.Name
			})

			if got, want := stats, tc.expectedStats; !cmp.Equal(got, want) {
				t.Errorf("Unexpected stats (-want +got): %s", cmp.Diff(want, got))
			}
		})
	}
}

func TestConcurrencyReporterRun(t *testing.T) {
	cr, ctx, cancel := newTestReporter(t)
	defer cancel()

	reportCh := make(chan time.Time)
	go func() {
		cr.run(ctx.Done(), reportCh)
		close(reportCh)
	}()

	now := time.Time{}
	cr.handleEvent(network.ReqEvent{Key: rev1, Type: network.ReqIn, Time: now})
	cr.handleEvent(network.ReqEvent{Key: rev1, Type: network.ReqIn, Time: now})
	cr.handleEvent(network.ReqEvent{Key: rev1, Type: network.ReqIn, Time: now})

	reportCh <- now.Add(1)

	want := []metrics.StatMessage{{
		Key: rev1,
		Stat: metrics.Stat{
			AverageConcurrentRequests: 1,
			RequestCount:              1,
			PodName:                   activatorPodName,
		},
	}, {
		Key: rev1,
		Stat: metrics.Stat{
			AverageConcurrentRequests: 2, // Discounted via the from 0 stat.
			RequestCount:              2, // Discounted via the from 0 stat.
			PodName:                   activatorPodName,
		},
	}}

	reportCh <- time.Now()

	got := make([]metrics.StatMessage, 0, len(want))
	got = append(got, <-cr.statCh...) // Scale from 0.
	got = append(got, <-cr.statCh...) // Actual report.
	if !cmp.Equal(got, want) {
		t.Errorf("Unexpected stats (-want +got): %s", cmp.Diff(want, got))
	}
}

func TestConcurrencyReporterHandler(t *testing.T) {
	cr, ctx, cancel := newTestReporter(t)
	defer cancel()

	reportCh := make(chan time.Time)
	go func() {
		cr.run(ctx.Done(), reportCh)
		close(reportCh)
	}()

	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := cr.Handler(baseHandler)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com", nil)
	rCtx := util.WithRevID(context.Background(), rev1)

	// Send a few requests.
	handler.ServeHTTP(resp, req.WithContext(rCtx))
	handler.ServeHTTP(resp, req.WithContext(rCtx))
	handler.ServeHTTP(resp, req.WithContext(rCtx))

	want := []metrics.StatMessage{{
		Key: rev1,
		Stat: metrics.Stat{
			AverageConcurrentRequests: 1,
			RequestCount:              1,
			PodName:                   activatorPodName,
		},
	}, {
		Key: rev1,
		Stat: metrics.Stat{
			AverageConcurrentRequests: 0, // Discounted via the from 0 stat.
			RequestCount:              2, // Discounted via the from 0 stat.
			PodName:                   activatorPodName,
		},
	}}

	reportCh <- time.Now()

	got := make([]metrics.StatMessage, 0, len(want))
	got = append(got, <-cr.statCh...) // Scale from 0.
	got = append(got, <-cr.statCh...) // Actual report.
	if !cmp.Equal(got, want) {
		t.Errorf("Unexpected stats (-want +got): %s", cmp.Diff(want, got))
	}
}

func TestMetricsReported(t *testing.T) {
	reset()
	cr, _, cancel := newTestReporter(t)
	defer cancel()

	cr.handleEvent(network.ReqEvent{Key: rev1, Type: network.ReqIn})
	cr.handleEvent(network.ReqEvent{Key: rev1, Type: network.ReqIn})
	cr.handleEvent(network.ReqEvent{Key: rev1, Type: network.ReqIn})
	cr.handleEvent(network.ReqEvent{Key: rev1, Type: network.ReqIn})
	cr.report(time.Time{}.Add(1 * time.Millisecond))

	wantTags := map[string]string{
		metricskey.LabelRevisionName:      rev1.Name,
		metricskey.LabelNamespaceName:     rev1.Namespace,
		metricskey.LabelServiceName:       "service-" + rev1.Name,
		metricskey.LabelConfigurationName: "config-" + rev1.Name,
		metricskey.PodName:                "the-best-activator",
		metricskey.ContainerName:          "activator",
	}
	metricstest.CheckLastValueData(t, "request_concurrency", wantTags, 4)
}

func newTestReporter(t *testing.T) (*ConcurrencyReporter, context.Context, context.CancelFunc) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	revisionInformer(ctx, revision(rev1.Namespace, rev1.Name),
		revision(rev2.Namespace, rev2.Name), revision(rev3.Namespace, rev3.Name))

	// Buffered channel permits avoiding sending the test commands on the separate go routine
	// simplifying main test process.
	statCh := make(chan []metrics.StatMessage, 10)
	return NewConcurrencyReporter(ctx, activatorPodName, statCh), ctx, cancel
}

func revisionInformer(ctx context.Context, revs ...*v1.Revision) {
	fake := fakeservingclient.Get(ctx)
	revisions := fakerevisioninformer.Get(ctx)

	for _, rev := range revs {
		fake.ServingV1().Revisions(rev.Namespace).Create(rev)
		revisions.Informer().GetIndexer().Add(rev)
	}
}

func BenchmarkConcurrencyReporter(b *testing.B) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(b)
	defer cancel()

	// Buffer equal to the activator.
	statCh := make(chan []asmetrics.StatMessage)
	cr := NewConcurrencyReporter(ctx, activatorPodName, statCh)

	stopCh := make(chan struct{})
	defer close(stopCh)
	go cr.Run(stopCh)

	// Just read and ignore all stat messages.
	go func() {
		for {
			select {
			case <-statCh:
			case <-stopCh:
				return
			}
		}
	}()

	// Spread the load across 100 revisions.
	keys := make([]types.NamespacedName, 0, 100)
	for i := 0; i < cap(keys); i++ {
		keys = append(keys, types.NamespacedName{
			Namespace: testNamespace,
			Name:      testRevName + strconv.Itoa(i),
		})
	}

	request := func(key types.NamespacedName) {
		cr.handleEvent(network.ReqEvent{
			Time: time.Now(),
			Type: network.ReqIn,
			Key:  key,
		})
		cr.handleEvent(network.ReqEvent{
			Time: time.Now(),
			Type: network.ReqOut,
			Key:  key,
		})
	}

	b.Run("sequential", func(b *testing.B) {
		for j := 0; j < b.N; j++ {
			key := keys[j%len(keys)]
			request(key)
		}
	})

	b.Run("parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			var j int
			for pb.Next() {
				key := keys[j%len(keys)]
				request(key)
				j++
			}
		})
	})
}
