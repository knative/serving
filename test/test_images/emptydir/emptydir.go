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
	"fmt"
	"log"
	"net/http"
	"os"

	"knative.dev/serving/test"
)

var path string

// Add content to a file in the emptyDir volume
func init() {
	path = os.Getenv("DATA_PATH")
	if path == "" {
		path = "/data"
	}
	f, err := os.OpenFile(path+"/testfile", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Print("Failed to open file", err)
	}
	_, _ = f.WriteString("From file in empty dir!")
	defer f.Close()
}

func handler(w http.ResponseWriter, r *http.Request) {
	content, err := os.ReadFile(path + "/testfile")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = fmt.Fprintln(w, string(content))
}

func main() {
	flag.Parse()
	log.Print("Empty dir volume app started.")
	test.ListenAndServeGracefully(":8080", handler)
}
