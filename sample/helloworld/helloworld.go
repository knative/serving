/*
Copyright 2018 Google LLC

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
	"time"
)

func handler(w http.ResponseWriter, r *http.Request) {
	log.Print("Hello world received a request.")

	target := os.Getenv("TARGET")
	if target == "" {
		target = "NOT SPECIFIED"
	}
	fmt.Fprintf(w, "Hello World: %s!\n", target)
}

func logHandler(w http.ResponseWriter, r *http.Request) {
	timestamp := time.Now()

	// Send logs to STDOUT
	msg := "A log in plain text format to STDOUT"
	fmt.Fprintln(os.Stdout, msg)

	data := map[string]string{
		"log":  "A log in json format to STDOUT",
		"time": timestamp.String(),
	}
	jsonOutput, _ := json.Marshal(data)
	fmt.Fprintln(os.Stdout, string(jsonOutput))

	// Send logs to /var/log
	fileName := "/var/log/sample.log"
	f, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	defer f.Close()

	if err == nil {
		msg = "A log in plain text format to /var/log\n"
		if _, err := f.WriteString(msg); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write to %s: %v", fileName, err)
		}

		data := map[string]string{
			"log":  "A log in json format to /var/log",
			"time": timestamp.String(),
			// fluentd-time is the reserved key to tell fluentd the logging time. It
			// must be in the format of RFC3339Nano, i.e. %Y-%m-%dT%H:%M:%S.%NZ.
			// Without this, fluentd uses current time when it collect the log as an
			// event time.
			"fluentd-time": timestamp.Format(time.RFC3339Nano),
		}
		jsonOutput, _ := json.Marshal(data)
		if _, err := f.WriteString(string(jsonOutput) + "\n"); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write to %s: %v", fileName, err)
		}
		f.Sync()
	} else {
		fmt.Fprintf(os.Stderr, "Failed to create %s: %v", fileName, err)
	}

	fmt.Fprint(w, "Sending logs done.\n")
}

func main() {
	flag.Parse()
	log.Print("Hello world sample started.")

	http.HandleFunc("/", handler)
	http.HandleFunc("/logs", logHandler)
	http.ListenAndServe(":8080", nil)
}
