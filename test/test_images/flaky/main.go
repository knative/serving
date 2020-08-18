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

package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"

	network "knative.dev/networking/pkg"
	"knative.dev/serving/test"
)

var (
	period uint64
	count  uint64
)

func handler(w http.ResponseWriter, r *http.Request) {
	// Always succeed probes.
	if network.IsKubeletProbe(r) {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Increment the request count per non-probe request.
	val := atomic.AddUint64(&count, 1)

	if val%period > 0 {
		w.WriteHeader(http.StatusInternalServerError)
	}
	w.Write([]byte(fmt.Sprintf("count = %d", val)))
}

func main() {
	p, err := strconv.Atoi(os.Getenv("PERIOD"))
	if err != nil {
		log.Fatal("Must specify PERIOD environment variable.")
	} else if p < 1 {
		log.Fatalf("Period must be positive, got: %d", p)
	}
	period = uint64(p)
	h := network.NewProbeHandler(http.HandlerFunc(handler))
	test.ListenAndServeGracefully(":"+os.Getenv("PORT"), h.ServeHTTP)
}
