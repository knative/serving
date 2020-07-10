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

package conformance

type state uint32

const (
	alpha state = 1 << iota
	beta
)

// OptionFunc are functions that modify the conformance
// test options
type OptionFunc func(*Options) error

// Options contains various properties that influence
// what tests should be exercised
type Options struct {
	testStates state
}

// BetaFeaturesEnabled returns true if the conformance
// test should include beta features
func (o *Options) BetaFeaturesEnabled() bool {
	return o.testStates&beta != 0
}

// AlphaFeaturesEnabled returns true if the conformance
// test should include alpha features
func (o *Options) AlphaFeaturesEnabled() bool {
	return o.testStates&alpha != 0
}

// NewOptions computes the options for a conformance test
func NewOptions(funcs ...OptionFunc) (Options, error) {
	var opts Options

	for _, optFunc := range funcs {
		if err := optFunc(&opts); err != nil {
			return Options{}, err
		}
	}

	return opts, nil
}

// EnableAlphaFeatures option will cause the conformance test
// to run execise alpha features
func EnableAlphaFeatures() OptionFunc {
	return func(o *Options) error {
		o.testStates |= alpha
		return nil
	}
}

// EnableBetaFeatures option will cause the conformance test
// to run execise beta features
func EnableBetaFeatures() OptionFunc {
	return func(o *Options) error {
		o.testStates |= beta
		return nil
	}
}
