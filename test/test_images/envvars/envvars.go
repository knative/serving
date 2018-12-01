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

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/knative/serving/test"
)

var SHOULD_SET = []string{ "K_SERVICE", "K_CONFIGURATION", "K_REVISION"}
var MUST_SET = []string{"PORT"}

func fetchEnvVariables(envVars []string) map[string]string {
	m := make(map[string]string)
	for _, envVar := range envVars {
		m[envVar] = os.Getenv(envVar)
	}

	return m
}

func handlRequest(w http.ResponseWriter, envVarsSet []string) {
	envVars := fetchEnvVariables(envVarsSet)
	resp, err := json.Marshal(envVars)
	if err != nil {
		fmt.Fprintf(w, fmt.Sprint("error building response : %v", err))
	}
	fmt.Fprintf(w, string(resp))
}

func shouldSetHandler(w http.ResponseWriter, r *http.Request) {
	handlRequest(w, SHOULD_SET)
}

func mustSetHandler(w http.ResponseWriter, r *http.Request) {
	handlRequest(w, MUST_SET)
}

func main() {
	flag.Parse()
	log.Print("Env vars test app started.")
	test.ListenAndServeGracefullyWithPattern(":8080", map[string]func(w http.ResponseWriter, r *http.Request){
		"/envvars/should": shouldSetHandler,
		"/envvars/must": mustSetHandler,
	})
}
