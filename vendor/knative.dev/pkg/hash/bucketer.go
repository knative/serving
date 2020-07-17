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

// This file contains the utilities to make bucketing decisions.

package hash

import (
	"errors"
	"sync"

	lru "github.com/hashicorp/golang-lru"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/pkg/reconciler"
)

var _ reconciler.Bucket = (*Bucket)(nil)

// BucketSet answers to what bucket does key X belong in a
// consistent manner (consistent as in consistent hashing).
type BucketSet struct {
	// Stores the cached lookups. cache is internally thread safe.
	cache *lru.Cache

	// mu guards buckets.
	mu sync.RWMutex
	// All the bucket names. Needed for building hash universe.
	buckets sets.String
}

// Bucket implements reconciler.Bucket and wraps around BuketSet
// for bucketing functions.
type Bucket struct {
	name string
	// `name` must be in this BucketSet.buckets.
	buckets *BucketSet
}

// Scientifically inferred preferred cache size.
const cacheSize = 4096

func newCache() *lru.Cache {
	c, _ := lru.New(cacheSize)
	return c
}

// NewBucketSet creates a new bucket set with the given universe
// of bucket names.
func NewBucketSet(bucketList sets.String) *BucketSet {
	return &BucketSet{
		cache:   newCache(),
		buckets: bucketList,
	}
}

// NewBucket creates a new bucket. Caller MUST make sure that
// the given `name` is in the given `bl.buckets`.
func NewBucket(name string, bl *BucketSet) *Bucket {
	return &Bucket{
		name:    name,
		buckets: bl,
	}
}

// Name implements Bucket.
func (b *Bucket) Name() string {
	return b.name
}

// Has returns true if this bucket owns the key and
// implements reconciler.Bucket interface.
func (b *Bucket) Has(nn types.NamespacedName) bool {
	return b.buckets.Owner(nn.String()) == b.name
}

// NewBucket creates a new Bucket with given name based on this bucketset.
func (bs *BucketSet) NewBucket(name string) (*Bucket, error) {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	if !bs.buckets.Has(name) {
		return nil, errors.New(name + " is not a valid bucket in the bucketset")
	}
	return &Bucket{
		name:    name,
		buckets: bs,
	}, nil
}

// Owner returns the owner of the key.
// Owner will cache the results for faster lookup.
func (bs *BucketSet) Owner(key string) string {
	if v, ok := bs.cache.Get(key); ok {
		return v.(string)
	}
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	l := ChooseSubset(bs.buckets, 1 /*single query wanted*/, key)
	ret := l.UnsortedList()[0]
	bs.cache.Add(key, ret)
	return ret
}

// BucketList returns the bucket names of this BucketSet in random order.
func (bs *BucketSet) BucketList() []string {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	return bs.buckets.UnsortedList()
}

// Update updates the universe of buckets.
func (bs *BucketSet) Update(newB sets.String) {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	// In theory we can iterate over the map and
	// purge only the keys that moved to a new shard.
	// But this might be more expensive than re-build
	// the cache as reconciliations happen.
	bs.cache.Purge()
	bs.buckets = newB
}
