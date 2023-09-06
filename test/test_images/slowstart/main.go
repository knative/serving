/*
Copyright 2023 The Knative Authors

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
	"time"
)

func main() {
	sleep := flag.Int("sleep", 0, "Time to sleep in seconds")
	flag.Parse()

	duration, err := time.ParseDuration(fmt.Sprintf("%ds", *sleep))
	if err != nil {
		log.Fatalf("Could not parse duration: %d, err: %v", *sleep, err)
	}
	time.Sleep(duration)

	log.Printf("Sleept for: %ds", *sleep)
}
