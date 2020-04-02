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
	"sort"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"k8s.io/apimachinery/pkg/types"

	"knative.dev/pkg/metrics/metricskey"
	"knative.dev/pkg/metrics/metricstest"
	rtesting "knative.dev/pkg/reconciler/testing"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/autoscaler/metrics"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
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
	op  int
	key types.NamespacedName
}

func TestStats(t *testing.T) {
	tt := []struct {
		name          string
		ops           []reqOp
		expectedStats []metrics.StatMessage
	}{{
		name: "Scale-from-zero sends stat",
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
			}}, {
			Key: rev2,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   activatorPodName,
			}},
		}}, {
		name: "in'n out",
		ops: []reqOp{{
			op:  requestOpStart,
			key: rev1,
		}, {
			op:  requestOpEnd,
			key: rev1,
		}, {
			op:  requestOpStart,
			key: rev1,
		}, {
			op:  requestOpEnd,
			key: rev1,
		}, {
			op: requestOpTick, // This won't result in reporting anything at all.
		}},
		expectedStats: []metrics.StatMessage{{
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   activatorPodName,
			}}, {
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 0,
				RequestCount:              1,
				PodName:                   activatorPodName,
			}},
		}}, {
		name: "Scale to two",
		ops: []reqOp{{
			op:  requestOpStart,
			key: rev1,
		}, {
			op:  requestOpStart,
			key: rev1,
		}, {
			op: requestOpTick,
		}, {
			op: requestOpTick,
		}},
		expectedStats: []metrics.StatMessage{{
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   activatorPodName,
			}}, {
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1, // We subtract the one concurrent request we already reported.
				RequestCount:              1,
				PodName:                   activatorPodName,
			}}, {
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 2, // Next reporting period, report both requests in flight.
				RequestCount:              0, // No new requests have appeared.
				PodName:                   activatorPodName,
			}},
		}}, {
		name: "Scale-from-zero after tick sends stat",
		ops: []reqOp{{
			op:  requestOpStart,
			key: rev1,
		}, {
			op:  requestOpEnd,
			key: rev1,
		}, {
			op: requestOpTick,
		}, {
			op:  requestOpStart,
			key: rev1,
		}},
		expectedStats: []metrics.StatMessage{{
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   activatorPodName,
			}}, {
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 0,
				RequestCount:              0,
				PodName:                   activatorPodName,
			}}, {
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   activatorPodName,
			}},
		}}, {
		name: "Multiple revisions tick",
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
			}}, {
			Key: rev2,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   activatorPodName,
			}}, {
			Key: rev3,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   activatorPodName,
			}}, {
			Key: rev1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 0,
				RequestCount:              0,
				PodName:                   activatorPodName,
			}}, {
			Key: rev2,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 0,
				RequestCount:              0,
				PodName:                   activatorPodName,
			}}, {
			Key: rev3,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 0,
				RequestCount:              0,
				PodName:                   activatorPodName,
			}},
		}},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			s, cr, ctx, cancel := newTestStats(t)
			defer cancel()
			go func() {
				cr.run(ctx.Done(), s.reportBiChan)
				close(s.reportBiChan)
			}()

			// Apply request operations
			for _, op := range tc.ops {
				switch op.op {
				case requestOpStart:
					s.reqChan <- ReqEvent{Key: op.key, EventType: ReqIn}
				case requestOpEnd:
					s.reqChan <- ReqEvent{Key: op.key, EventType: ReqOut}
				case requestOpTick:
					s.reportBiChan <- time.Time{}
				}
			}

			// Gather reported stats.
			stats := make([]metrics.StatMessage, 0, len(tc.expectedStats))
			for len(stats) < len(tc.expectedStats) {
				select {
				case x := <-s.statChan:
					stats = append(stats, x...)
				case <-time.After(time.Second):
					t.Fatal("Timed out waiting for the event")
				}
			}

			// Verify we're not getting extra events.
			select {
			case x := <-s.statChan:
				t.Fatalf("Extra events received: %v", x)
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

func TestMetricsReported(t *testing.T) {
	reset()
	s, cr, ctx, cancel := newTestStats(t)
	defer cancel()
	go func() {
		cr.run(ctx.Done(), s.reportBiChan)
		close(s.reportBiChan)
	}()

	s.reqChan <- ReqEvent{Key: rev1, EventType: ReqIn}
	s.reqChan <- ReqEvent{Key: rev1, EventType: ReqIn}
	s.reqChan <- ReqEvent{Key: rev1, EventType: ReqIn}
	s.reqChan <- ReqEvent{Key: rev1, EventType: ReqIn}
	s.reportBiChan <- time.Time{}
	<-s.statChan // The scale from 0 quick-report
	<-s.statChan // The actual report we want to see

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

// Test type to hold the bi-directional time channels
type testStats struct {
	reqChan      chan ReqEvent
	statChan     chan []metrics.StatMessage
	reportBiChan chan time.Time
}

func newTestStats(t *testing.T) (*testStats, *ConcurrencyReporter, context.Context, context.CancelFunc) {
	reportBiChan := make(chan time.Time)
	ts := &testStats{
		reqChan: make(chan ReqEvent),
		// Buffered channel permits avoiding sending the test commands on the separate go routine
		// simplifying main test process.
		statChan:     make(chan []metrics.StatMessage, 10),
		reportBiChan: reportBiChan,
	}
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	revisionInformer(ctx, revision(rev1.Namespace, rev1.Name),
		revision(rev2.Namespace, rev2.Name), revision(rev3.Namespace, rev3.Name))

	return ts, NewConcurrencyReporter(ctx, activatorPodName,
		ts.reqChan, ts.statChan), ctx, cancel
}

func revisionInformer(ctx context.Context, revs ...*v1.Revision) {
	fake := fakeservingclient.Get(ctx)
	revisions := fakerevisioninformer.Get(ctx)

	for _, rev := range revs {
		fake.ServingV1().Revisions(rev.Namespace).Create(rev)
		revisions.Informer().GetIndexer().Add(rev)
	}
}
