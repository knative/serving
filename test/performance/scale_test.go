// +build performance

/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// scale_test.go brings up a number of services tracking the time to various waypoints.

package performance

import (
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"knative.dev/pkg/test/junit"
	perf "knative.dev/pkg/test/performance"
	"knative.dev/pkg/test/testgrid"

	"knative.dev/serving/test/e2e"
)

type metrics struct {
	minV          time.Duration
	maxV          time.Duration
	totalDuration time.Duration
	numV          int64
}

func (m *metrics) add(start time.Time) {
	// Compute the duration since the provided start time.
	d := time.Since(start)

	if d < m.minV || m.minV == 0 {
		m.minV = d
	}
	if d > m.maxV {
		m.maxV = d
	}
	m.totalDuration += d
	m.numV++
}

func (m metrics) min() float32 {
	return float32(m.minV.Seconds())
}

func (m metrics) max() float32 {
	return float32(m.maxV.Seconds())
}

func (m metrics) avg() float32 {
	if m.numV == 0 {
		return -1.0
	}
	return float32(m.totalDuration.Seconds()) / float32(m.numV)
}

func (m metrics) num() int64 {
	return m.numV
}

type latencies struct {
	// Guards access to latencies.
	m       sync.RWMutex
	metrics map[string]metrics
}

var _ e2e.Latencies = (*latencies)(nil)

// Add implements e2e.Latencies interface, hence is public.
func (l *latencies) Add(name string, start time.Time) {
	l.m.Lock()
	defer l.m.Unlock()

	m := l.metrics[name]
	m.add(start)
	l.metrics[name] = m
}

func (l *latencies) min(name string) float32 {
	l.m.RLock()
	defer l.m.RUnlock()
	return l.metrics[name].min()
}

func (l *latencies) max(name string) float32 {
	l.m.RLock()
	defer l.m.RUnlock()
	return l.metrics[name].max()
}

func (l *latencies) avg(name string) float32 {
	l.m.RLock()
	defer l.m.RUnlock()
	return l.metrics[name].avg()
}

func (l *latencies) num(name string) int64 {
	l.m.RLock()
	defer l.m.RUnlock()
	return l.metrics[name].num()
}

func (l *latencies) results(t *testing.T) []junit.TestCase {
	l.m.RLock()
	defer l.m.RUnlock()

	order := make([]string, 0, len(l.metrics))
	for k := range l.metrics {
		order = append(order, k)
	}
	sort.Strings(order)

	// Add latency metrics
	tc := make([]junit.TestCase, 0, 3*len(order))
	for _, key := range order {
		tc = append(tc,
			perf.CreatePerfTestCase(l.min(key), key+".min", t.Name()),
			perf.CreatePerfTestCase(l.max(key), key+".max", t.Name()),
			perf.CreatePerfTestCase(l.avg(key), key+".avg", t.Name()),
			perf.CreatePerfTestCase(float32(l.num(key)), key+".num", t.Name()))
	}
	return tc
}

func TestScaleToN(t *testing.T) {
	// Run each of these variations.
	tests := []int{10, 50, 100}

	// Accumulate the results from each row in our table (recorded below).
	var results []junit.TestCase

	for _, size := range tests {
		t.Run(fmt.Sprintf("scale-%03d", size), func(t *testing.T) {
			// Record the observed latencies.
			l := &latencies{
				metrics: make(map[string]metrics),
			}
			defer func() {
				results = append(results, l.results(t)...)
			}()

			e2e.ScaleToWithin(t, size, 30*time.Minute, l)
		})
	}

	if err := testgrid.CreateXMLOutput(results, t.Name()); err != nil {
		t.Errorf("Cannot create output xml: %v", err)
	}
}
