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

package fake

import (
	"time"

	"k8s.io/apimachinery/pkg/util/clock"
)

// A ManualTickProvider holds a channel that delivers `ticks' of a clock at intervals.
type ManualTickProvider struct {
	Channel chan time.Time
}

// NewTicker returns a Ticker containing a channel that will send the
// time with a period specified by the duration argument.
func (mtp *ManualTickProvider) NewTicker(time.Duration) *time.Ticker {
	return &time.Ticker{
		C: mtp.Channel,
	}
}

// C returns the tick channel.
func (mtp *ManualTickProvider) C() <-chan time.Time {
	return mtp.Channel
}

// Stop is nonce here.
func (mtp *ManualTickProvider) Stop() {}

// Clock is K8s clock.Clock but it overrides tick provider
// with ManualTickProvider above.
type Clock struct {
	*clock.FakeClock
	TP *ManualTickProvider
}

// NewTicker returns a NewTicker which is a ManualTickProvider.
func (fc Clock) NewTicker(time.Duration) clock.Ticker {
	return fc.TP
}
