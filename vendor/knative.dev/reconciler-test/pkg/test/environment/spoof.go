/*
Copyright 2020 The Knative Authors

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
package environment

import (
	"flag"
	"time"
)

type Spoof struct {
	RequestInterval time.Duration // SpoofRequestInterval is the interval between requests in SpoofingClient
	RequestTimeout  time.Duration // SpoofRequestTimeout is the timeout for polling requests in SpoofingClient
}

func (s *Spoof) AddFlags(fs *flag.FlagSet) {
	fs.DurationVar(&s.RequestInterval, "env.spoof.interval", 1*time.Second,
		"Provide an interval between requests for the SpoofingClient")

	fs.DurationVar(&s.RequestTimeout, "env.spoof.timeout", 5*time.Minute,
		"Provide a request timeout for the SpoofingClient")
}
