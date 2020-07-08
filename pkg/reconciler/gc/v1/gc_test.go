/*
Copyright 2020 The Knative Authors.

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

package v1

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	pkgrec "knative.dev/pkg/reconciler"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	gcconfig "knative.dev/serving/pkg/gc"
	"knative.dev/serving/pkg/reconciler/gc/config"

	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
)

func TestIsRevisionStale(t *testing.T) {
	curTime := time.Now()
	staleTime := curTime.Add(-10 * time.Minute)

	tests := []struct {
		name      string
		rev       *v1.Revision
		latestRev string
		want      bool
	}{{
		name: "fresh revision that was never pinned",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(curTime),
			},
		},
		want: false,
	}, {
		name: "stale revision that was never pinned w/ Ready status",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(staleTime),
			},
			Status: v1.RevisionStatus{
				Status: duckv1.Status{
					Conditions: duckv1.Conditions{{
						Type:   v1.RevisionConditionReady,
						Status: "True",
					}},
				},
			},
		},
		want: false,
	}, {
		name: "stale revision that was never pinned w/o Ready status",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(staleTime),
			},
			Status: v1.RevisionStatus{
				Status: duckv1.Status{
					Conditions: duckv1.Conditions{{
						Type:   v1.RevisionConditionReady,
						Status: "Unknown",
					}},
				},
			},
		},
		want: true,
	}, {
		name: "stale revision that was previously pinned",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(staleTime),
				Annotations: map[string]string{
					"serving.knative.dev/lastPinned": fmt.Sprintf("%d", staleTime.Unix()),
				},
			},
		},
		want: true,
	}, {
		name: "fresh revision that was previously pinned",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(staleTime),
				Annotations: map[string]string{
					"serving.knative.dev/lastPinned": fmt.Sprintf("%d", curTime.Unix()),
				},
			},
		},
		want: false,
	}, {
		name: "stale latest ready revision",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(staleTime),
				Annotations: map[string]string{
					"serving.knative.dev/lastPinned": fmt.Sprintf("%d", staleTime.Unix()),
				},
			},
		},
		latestRev: "myrev",
		want:      false,
	}}

	cfgStore := testConfigStore{
		config: &config.Config{
			RevisionGC: &gcconfig.Config{
				StaleRevisionCreateDelay:        5 * time.Minute,
				StaleRevisionTimeout:            5 * time.Minute,
				StaleRevisionMinimumGenerations: 2,
			},
		},
	}
	ctx := cfgStore.ToContext(context.Background())

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := &v1.Configuration{
				Status: v1.ConfigurationStatus{
					ConfigurationStatusFields: v1.ConfigurationStatusFields{
						LatestReadyRevisionName: test.latestRev,
					},
				},
			}

			got := isRevisionStale(ctx, test.rev, cfg)

			if got != test.want {
				t.Errorf("IsRevisionStale want %v got %v", test.want, got)
			}
		})
	}
}

type testConfigStore struct {
	config *config.Config
}

func (t *testConfigStore) ToContext(ctx context.Context) context.Context {
	return config.ToContext(ctx, t.config)
}

var _ pkgrec.ConfigStore = (*testConfigStore)(nil)
