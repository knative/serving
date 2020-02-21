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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"k8s.io/apimachinery/pkg/types"

	rtesting "knative.dev/pkg/reconciler/testing"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/autoscaler/metrics"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
)

const (
	requestOpTick  = "RequestOpTick"
	requestOpStart = "RequestOpStart"
	requestOpEnd   = "RequestOpEnd"
)

var (
	pod1 = types.NamespacedName{Namespace: "test", Name: "pod1"}
	pod2 = types.NamespacedName{Namespace: "test", Name: "pod2"}
	pod3 = types.NamespacedName{Namespace: "test", Name: "pod3"}
)

type reqOp struct {
	op   string
	key  types.NamespacedName
	time time.Time
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
			key: pod1,
		}, {
			op:  requestOpStart,
			key: pod2,
		}},
		expectedStats: []metrics.StatMessage{{
			Key: pod1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   "activator",
			}}, {
			Key: pod2,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   "activator",
			}},
		}}, {
		name: "Scale to two",
		ops: []reqOp{{
			op:  requestOpStart,
			key: pod1,
		}, {
			op:  requestOpStart,
			key: pod1,
		}, {
			op: requestOpTick,
		}},
		expectedStats: []metrics.StatMessage{{
			Key: pod1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   "activator",
			}}, {
			Key: pod1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 2,
				RequestCount:              2,
				PodName:                   "activator",
			}},
		}}, {
		name: "Scale-from-zero after tick sends stat",
		ops: []reqOp{{
			op:  requestOpStart,
			key: pod1,
		}, {
			op:  requestOpEnd,
			key: pod1,
		}, {
			op: requestOpTick,
		}, {
			op:  requestOpStart,
			key: pod1,
		}},
		expectedStats: []metrics.StatMessage{{
			Key: pod1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   "activator",
			}}, {
			Key: pod1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   "activator",
			}},
		}}, {
		name: "Multiple pods tick",
		ops: []reqOp{{
			op:  requestOpStart,
			key: pod1,
		}, {
			op:  requestOpStart,
			key: pod2,
		}, {
			op: requestOpTick,
		}, {
			op:  requestOpEnd,
			key: pod1,
		}, {
			op:  requestOpStart,
			key: pod3,
		}, {
			op: requestOpTick,
		}},
		expectedStats: []metrics.StatMessage{{
			Key: pod1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   "activator",
			}}, {
			Key: pod2,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   "activator",
			}}, {
			Key: pod1,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   "activator",
			}}, {
			Key: pod2,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   "activator",
			}}, {
			Key: pod3,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   "activator",
			}}, {
			Key: pod2,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              0,
				PodName:                   "activator",
			}}, {
			Key: pod3,
			Stat: metrics.Stat{
				AverageConcurrentRequests: 1,
				RequestCount:              1,
				PodName:                   "activator",
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

			go func() {
				// Apply request operations
				for _, op := range tc.ops {
					switch op.op {
					case requestOpStart:
						s.reqChan <- ReqEvent{Key: op.key, EventType: ReqIn}
					case requestOpEnd:
						s.reqChan <- ReqEvent{Key: op.key, EventType: ReqOut}
					case requestOpTick:
						s.reportBiChan <- op.time
					}
				}
			}()

			// Gather reported stats
			stats := make([]metrics.StatMessage, 0, len(tc.expectedStats))
			for len(stats) < len(tc.expectedStats) {
				stats = append(stats, <-s.statChan...)
			}

			// Check the stats we got match what we wanted
			sorter := cmpopts.SortSlices(func(a, b metrics.StatMessage) bool {
				return a.Key.Name < b.Key.Name
			})
			if got, want := stats, tc.expectedStats; !cmp.Equal(got, want, sorter) {
				t.Errorf("Unexpected stats (-want +got): %s", cmp.Diff(want, got, sorter))
			}
		})
	}
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
		reqChan:      make(chan ReqEvent),
		statChan:     make(chan []metrics.StatMessage),
		reportBiChan: reportBiChan,
	}
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	revisionInformer(ctx, revision(testNamespace, testRevName))

	cr := NewConcurrencyReporter(ctx, "activator", ts.reqChan, ts.statChan)
	return ts, cr, ctx, cancel
}

func revisionInformer(ctx context.Context, revs ...*v1.Revision) {
	fake := fakeservingclient.Get(ctx)
	revisions := fakerevisioninformer.Get(ctx)

	for _, rev := range revs {
		fake.ServingV1().Revisions(rev.Namespace).Create(rev)
		revisions.Informer().GetIndexer().Add(rev)
	}
}
