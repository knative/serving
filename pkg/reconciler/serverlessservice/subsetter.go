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
	"hash"
	"hash/fnv"
	"sort"
	"strconv"

	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	startSalt = "start-angle-salt"
	stepSalt  = "step-angle-salt"
	fromSalt  = "from-salt"

	// universe represents the possible range of angles [0, universe).
	// We want to have universe divide total range evenly to reduce bias.
	universe = (1 << 10)
)

// computeAngle returns a uint64 number swhich represents
// a hashe built off the given `n` string for consistent selection
// algorithm.
func computeHash(n string, h hash.Hash64) uint64 {
	h.Reset()
	h.Write([]byte(n))
	return h.Sum64()
}

type hashData struct {
	// The set of all hashes for fast lookup and to name mapping
	nameLookup map[int]string
	// Sorted set of hashes for selection algorithm.
	hashPool []int
	// start angle
	start int
	// step angle
	step int
}

func (hd *hashData) fromIndexSet(s sets.Int) sets.String {
	ret := sets.NewString()
	for v := range s {
		ret.Insert(hd.nameForHIndex(v))
	}
	return ret
}

func (hd *hashData) nameForHIndex(hi int) string {
	return hd.nameLookup[hd.hashPool[hi]]
}

func buildHashes(from []string, target string) *hashData {
	hasher := fnv.New64a()
	hd := &hashData{
		nameLookup: make(map[int]string, len(from)),
		hashPool:   make([]int, len(from)),
		start:      int(computeHash(target+startSalt, hasher) % universe),
		step:       int(computeHash(target+stepSalt, hasher) % universe),
	}

	for i, f := range from {
		// Make unique sets for every target.
		k := f + target
		h := int(computeHash(k, hasher))
		hs := h % universe
		// Two values slotted to the same bucket.
		// On average should happen with 1/universe probability.
		for _, ok := hd.nameLookup[hs]; ok; _, ok = hd.nameLookup[hs] {
			// Feed the hash as salt.
			k = f + strconv.Itoa(h)
			h = int(computeHash(k, hasher))
			hs = h % universe
		}

		hd.hashPool[i] = h
		hd.nameLookup[h] = f
	}
	// Sort for consistent mapping later.
	sort.Slice(hd.hashPool, func(i, j int) bool {
		return hd.hashPool[i] < hd.hashPool[j]
	})
	return hd
}

// chooseSubset consistently chooses n items from `from`, using
// `target` as a seed value.
// chooseSubset is an internal function and presumes sanitized inputs.
// TODO(vagababov): once initial impl is ready, think about how to cache
// the prepared data.
func chooseSubset(from []string, n int, target string) sets.String {
	if n >= len(from) {
		return sets.NewString(from...)
	}

	hashData := buildHashes(from, target)

	// The algorithm for selection does the following:
	// 0. Select angle to be the start angle
	// 1. While n candidates are not selected
	// 2. Find the index for that angle.
	//    2.1. While that index is already selected pick next index
	// 3. advance angle by `step`
	// 4. goto 1.
	selection := sets.NewInt()
	angle := hashData.start
	for len(selection) < n {
		root := sort.Search(len(from), func(i int) bool {
			return hashData.hashPool[i] >= angle
		})
		// Wrap around.
		if root == len(from) {
			root = 0
		}
		// Already matched this one. Continue to the next index.
		for selection.Has(root) {
			root = (root + 1) % len(from)
		}
		selection.Insert(root)
		angle = (angle + hashData.step) % universe
	}

	return hashData.fromIndexSet(selection)
}
