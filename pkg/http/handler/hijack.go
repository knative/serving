/*
Copyright 2025 The Knative Authors

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
	"cmp"
	"context"
	"net/http"
	"sync/atomic"
	"time"
)

type HijackTracker struct {
	Handler      http.Handler
	PollInterval time.Duration

	inflight atomic.Int64
}

func (s *HijackTracker) Drain(ctx context.Context) error {
	pollInterval := cmp.Or(s.PollInterval, time.Second)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		if s.inflight.Load() == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *HijackTracker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.inflight.Add(1)
	defer s.inflight.Add(-1)

	s.Handler.ServeHTTP(w, r)
}
