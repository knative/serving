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

package fake

import (
	"time"

	"k8s.io/apimachinery/pkg/types"
)

// MetricClient is a fake implementation of autoscaler.MetricClient for testing.
type MetricClient struct {
	StableConcurrency float64
	PanicConcurrency  float64
	StableRPS         float64
	PanicRPS          float64
	ErrF              func(key types.NamespacedName, now time.Time) error
}

// StableAndPanicConcurrency returns stable/panic concurrency stored in the object
// and the result of Errf as the error.
func (t *MetricClient) StableAndPanicConcurrency(key types.NamespacedName, now time.Time) (float64, float64, error) {
	var err error
	if t.ErrF != nil {
		err = t.ErrF(key, now)
	}
	return t.StableConcurrency, t.PanicConcurrency, err
}

// StableAndPanicRPS returns stable/panic RPS stored in the object
// and the result of Errf as the error.
func (t *MetricClient) StableAndPanicRPS(key types.NamespacedName, now time.Time) (float64, float64, error) {
	var err error
	if t.ErrF != nil {
		err = t.ErrF(key, now)
	}
	return t.StableRPS, t.PanicRPS, err
}

// StaticMetricClient returns stable/panic concurrency and RPS with static value, i.e. 10.
var StaticMetricClient = MetricClient{
	StableConcurrency: 10.0,
	PanicConcurrency:  10.0,
	StableRPS:         10.0,
	PanicRPS:          10.0,
}
