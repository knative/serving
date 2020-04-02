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
	"hash"
	"hash/fnv"
	"sort"
)

const (
	startSalt = "start-angle-salt"
	stepSalt  = "step-angle-salt"
	fromSalt  = "from-salt"

	// universe represents the possible range of angles [0, universe).
	universe = uint64(360)
)

// computeAngle returns a uint64 number swhich represents
// a hashe built off the given `n` string for consistent selection
// algorithm.
func computeHash(n string, h hash.Hash64) uint64 {
	h.Reset()
	h.Write([]byte(n))
	return h.Sum64()
}

func buildHashes(from []string, target string) (uint64, uint64, []uint64) {
	h := fnv.New64()
	poolHashes := make([]uint64, len(from))
	for i, f := range from {
		// Without sal FNV returns adjacent values, for adjacent keys.
		poolHashes[i] = computeHash(f+fromSalt, h)
	}
	sort.Slice(poolHashes, func(i, j int) bool {
		return poolHashes[i] < poolHashes[j]
	})
	return computeHash(target+startSalt, h), computeHash(target+stepSalt, h), poolHashes
}

// chooseSubset consistently chooses n items from `from`, using
// `target` as a seed value.
// TODO(vagababov): once initial impl is ready, think about how to cache
// the prepared data.
func chooseSubset(from []string, n int, target string) []string {
	if n >= len(from) {
		return from
	}
	start, step, poolHashes := buildHashes(from, target)
	fmt.Printf("Start = %v Stop = %v Hashes = %v", start, step, poolHashes)
	return from
}
