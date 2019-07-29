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

// FakeMetricClient is a fake implementation of MetricClient for testing.
type FakeMetricClient struct {
	StableConcurrency float64
	PanicConcurrency  float64
	StableOPS         float64
	PanicOPS          float64
	ErrF              func(key types.NamespacedName, now time.Time) error
}

// StableAndPanicConcurrency returns stable/panic concurrency stored in the object
// and the result of Errf as the error.
func (t *FakeMetricClient) StableAndPanicConcurrency(key types.NamespacedName, now time.Time) (float64, float64, error) {
	var err error
	if t.ErrF != nil {
		err = t.ErrF(key, now)
	}
	return t.StableConcurrency, t.PanicConcurrency, err
}

// StableAndPanicOPS returns stable/panic OPS stored in the object
// and the result of Errf as the error.
func (t *FakeMetricClient) StableAndPanicOPS(key types.NamespacedName, now time.Time) (float64, float64, error) {
	var err error
	if t.ErrF != nil {
		err = t.ErrF(key, now)
	}
	return t.StableOPS, t.PanicOPS, err
}

// StaticMetricClient returns stable/panic concurrency and OPS with static value, i.e. 10.
var StaticMetricClient = FakeMetricClient{
	StableConcurrency: 10.0,
	PanicConcurrency:  10.0,
	StableOPS:         10.0,
	PanicOPS:          10.0,
}
