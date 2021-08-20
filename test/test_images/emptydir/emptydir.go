/*
Copyright 2021 The Knative Authors

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
	"flag"
	"log"
	"net/http"
	"os"
	"path"

	"knative.dev/serving/test"
)

func testfilePath() string {
	base := os.Getenv("DATA_PATH")
	if base == "" {
		base = "/data"
	}
	return path.Join(base, "testfile")
}

// Add content to a file in the emptyDir volume
func init() {
	if err := os.WriteFile(testfilePath(), []byte(test.EmptyDirText), 0644); err != nil {
		panic(err)
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	content, err := os.ReadFile(testfilePath())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(content)
}

func main() {
	flag.Parse()
	log.Print("Empty dir volume app started.")
	test.ListenAndServeGracefully(":8080", handler)
}
