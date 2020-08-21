/*
Copyright 2018 The Knative Authors

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

// The singlethreaded program
package main

import (
	"fmt"
	"net/http"
	"time"

	"go.uber.org/atomic"

	"knative.dev/serving/test"
)

var lockedFlag = atomic.NewInt32(-1)

func handler(w http.ResponseWriter, r *http.Request) {
	v := lockedFlag.Inc() // Returns the new value.
	defer lockedFlag.Dec()
	if v > 0 {
		// Return HTTP 500 if more than 1 request at a time gets in
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	time.Sleep(500 * time.Millisecond)
	fmt.Fprintf(w, "One at a time")
}

func main() {
	test.ListenAndServeGracefully(":8080", handler)
}
