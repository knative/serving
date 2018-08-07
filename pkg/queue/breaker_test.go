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

package queue

import (
	"reflect"
	"runtime"
	"testing"
)

func TestBreakerNoOverload(t *testing.T) {
	b := NewBreaker(1, 1)                  // Breaker capacity = 2
	want := []bool{true, true, true, true} // Only two requests will be in flight at a time

	r1, g1 := b.concurrentRequest()
	r2, g2 := b.concurrentRequest()
	done(r1)
	r3, g3 := b.concurrentRequest()
	done(r2)
	r4, g4 := b.concurrentRequest()
	done(r3)
	done(r4)
	got := []bool{<-g1, <-g2, <-g3, <-g4}

	if !reflect.DeepEqual(want, got) {
		t.Fatalf("Wanted %v. Got %v.", want, got)
	}
}

func (b *Breaker) concurrentRequest() (chan struct{}, chan bool) {
	release := make(chan struct{})
	thunk := func() {
		_, _ = <-release
	}
	result := make(chan bool)
	go func() {
		result <- b.Maybe(thunk)
	}()
	runtime.Gosched()
	return release, result
}

func done(release chan struct{}) {
	close(release)
	// Allow enough time for the two goroutines involved to shuffle
	// the request out of active and another request out of the
	// queue.
	runtime.Gosched()
	runtime.Gosched()
}
