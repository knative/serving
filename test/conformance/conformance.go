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

type OptionFunc func(*Options) error

type Options struct {
	testStates state
}

func (o *Options) BetaFeaturesEnabled() bool {
	return o.testStates&beta != 0
}

func (o *Options) AlphaFeaturesEnabled() bool {
	return o.testStates&alpha != 0
}

func NewOptions(funcs ...OptionFunc) (Options, error) {
	var opts Options

	for _, optFunc := range funcs {
		if err := optFunc(&opts); err != nil {
			return Options{}, err
		}
	}

	return opts, nil
}

func EnableAlphaFeatures() OptionFunc {
	return func(o *Options) error {
		o.testStates |= alpha
		return nil
	}
}

func EnableBetaFeatures() OptionFunc {
	return func(o *Options) error {
		o.testStates |= beta
		return nil
	}
}
