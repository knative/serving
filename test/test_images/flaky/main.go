/*
Copyright 2019 The Knative Authors

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

package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"go.uber.org/atomic"

	network "knative.dev/networking/pkg"
	"knative.dev/serving/test"
)

var (
	period uint64
	count  = atomic.NewUint64(0)
)

func handler(w http.ResponseWriter, r *http.Request) {
	// Always succeed probes.
	if network.IsKubeletProbe(r) {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Increment the request count per non-probe request.
	val := count.Inc()

	if val%period > 0 {
		w.WriteHeader(http.StatusInternalServerError)
	}
	w.Write([]byte(fmt.Sprint("count = ", val)))
}

func main() {
	pstr, ok := os.LookupEnv("PERIOD")
	if !ok {
		log.Fatal("Must specify PERIOD environment variable.")
	}
	var err error
	period, err = strconv.ParseUint(pstr, 10 /*base*/, 64 /*size*/)
	if err != nil {
		log.Fatal(`Error parsing "PERIOD" envvar as uint: `, err)
	} else if period < 1 {
		log.Fatal("Period must be positive, got: 0")
	}
	h := network.NewProbeHandler(http.HandlerFunc(handler))
	test.ListenAndServeGracefully(":"+os.Getenv("PORT"), h.ServeHTTP)
}
