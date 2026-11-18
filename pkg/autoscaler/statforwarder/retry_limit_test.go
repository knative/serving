/*
Copyright 2026 The Knative Authors

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

package statforwarder

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	logtesting "knative.dev/pkg/logging/testing"
)

func TestForwarderRetryLimit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		retry     int
		wantRetry bool
	}{
		{name: "first_retry", retry: 0, wantRetry: true},
		{name: "at_limit", retry: maxProcessingRetry, wantRetry: true},
		{name: "over_limit", retry: maxProcessingRetry + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := &Forwarder{statCh: make(chan stat, 1)}
			f.maybeRetry(logtesting.TestLogger(t), stat{sm: stat1, retry: tc.retry})
			// Wait for any scheduled retry, rather than relying on a sleep to
			// conclude that an over-limit stat was actually dropped.
			f.retryWg.Wait()
			select {
			case got := <-f.statCh:
				if !tc.wantRetry {
					t.Fatal("stat exceeding the retry limit was re-enqueued")
				}
				if got.retry != tc.retry+1 {
					t.Errorf("retry count = %d, want %d", got.retry, tc.retry+1)
				}
				if diff := cmp.Diff(stat1, got.sm); diff != "" {
					t.Errorf("retried stat changed (-want +got): %s", diff)
				}
			default:
				if tc.wantRetry {
					t.Fatal("stat within the retry limit was not re-enqueued")
				}
			}
		})
	}
}
