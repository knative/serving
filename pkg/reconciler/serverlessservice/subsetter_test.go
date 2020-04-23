/*
Copyright 2020 The Knative Authors

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

package serverlessservice

import (
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestBuildHashes(t *testing.T) {
	const target = "a target to remember"
	set := []string{"a", "b", "c", "e", "f"}

	hd1 := buildHashes(set, target)
	hd2 := buildHashes(set, target)
	t.Log("HashData = ", spew.Sprintf("%+v", hd1))

	if !cmp.Equal(hd1, hd2, cmp.AllowUnexported(hashData{})) {
		t.Errorf("buildHashe is not consistent: diff(-want,+got):\n%s",
			cmp.Diff(hd1, hd2, cmp.AllowUnexported(hashData{})))
	}
	if !sort.SliceIsSorted(hd1.hashPool, func(i, j int) bool {
		return hd1.hashPool[i] < hd1.hashPool[j]
	}) {
		t.Errorf("From list is not sorted: %v", hd1.hashPool)
	}
}

func TestChooseSubset(t *testing.T) {
	tests := []struct {
		name    string
		from    []string
		target  string
		wantNum int
		want    sets.String
	}{{
		name:    "return all",
		from:    []string{"sun", "moon", "mars", "mercury"},
		target:  "a target!",
		wantNum: 4,
		want:    sets.NewString("sun", "moon", "mars", "mercury"),
	}, {
		name:    "subset 1",
		from:    []string{"sun", "moon", "mars", "mercury"},
		target:  "a target!",
		wantNum: 2,
		want:    sets.NewString("mercury", "moon"),
	}, {
		name:    "subset 2",
		from:    []string{"sun", "moon", "mars", "mercury"},
		target:  "something else entirely",
		wantNum: 2,
		want:    sets.NewString("mercury", "mars"),
	}, {
		name:    "select 3",
		from:    []string{"sun", "moon", "mars", "mercury"},
		target:  "something else entirely",
		wantNum: 3,
		want:    sets.NewString("mars", "mercury", "sun"),
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := chooseSubset(tc.from, tc.wantNum, tc.target)
			if !got.Equal(tc.want) {
				t.Errorf("Chose = %v, want = %v, diff(-want,+got):\n%s", got, tc.want, cmp.Diff(tc.want, got))
			}
		})
	}
}

func TestCollisionHandling(t *testing.T) {
	const (
		key1   = "b08006d4-81f9-42ee-808b-ea18a39cbd83"
		key2   = "c9dc8df4-8c8d-4077-8750-6d2c2113a23b"
		target = "e68a64e1-19d8-4855-9ffa-04f49223a059"
	)
	// Verify baseline, that they collide.
	hasher := fnv.New64a()
	h1 := computeHash([]byte(key1+target), hasher) % universe
	hasher.Reset()
	h2 := computeHash([]byte(key2+target), hasher) % universe
	if h1 != h2 {
		t.Fatalf("Baseline incorrect keys don't collide %d != %d", h1, h2)
	}
	hd := buildHashes([]string{key1, key2}, target)
	if got, want := len(hd.nameLookup), 2; got != want {
		t.Error("Did not resolve collision, only 1 key in the map")
	}
}

func TestOverlay(t *testing.T) {
	// Execute
	// `go test -run=TestOverlay -count=200`
	// To ensure assignments are still not skewed.
	const (
		sources   = 50
		samples   = 30000
		selection = 10
		want      = samples * selection / sources
		threshold = want / 5 // 20%
	)
	from := make([]string, sources)
	for i := 0; i < sources; i++ {
		from[i] = uuid.New().String()
	}
	freqs := make(map[string]int, sources)

	for i := 0; i < samples; i++ {
		target := uuid.New().String()
		got := chooseSubset(from, selection, target)
		for k := range got {
			freqs[k]++
		}
	}

	totalDiff := 0.
	for _, v := range freqs {
		diff := float64(v - want)
		adiff := math.Abs(diff)
		totalDiff += adiff
		if adiff > threshold {
			t.Errorf("Diff for %d is %v, larger than threshold: %d", v, diff, threshold)
		}
	}
	t.Log(totalDiff / float64(len(freqs)))
}

func BenchmarkSelection(b *testing.B) {
	const maxSet = 200
	from := make([]string, maxSet)
	for i := 0; i < maxSet; i++ {
		from[i] = uuid.New().String()
	}
	for _, v := range []int{5, 10, 25, 50, 100, 150, maxSet} {
		for _, ss := range []int{1, 5, 10, 15, 20, 25} {
			b.Run(fmt.Sprintf("pool-%d-subset-%d", v, ss), func(b *testing.B) {
				target := uuid.New().String()
				for i := 0; i < b.N; i++ {
					chooseSubset(from[:v], 10, target)
				}
			})
		}
	}
}
