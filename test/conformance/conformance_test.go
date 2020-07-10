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

import "testing"

func TestAlphaOption(t *testing.T) {
	opts := &Options{}

	if err := EnableAlphaFeatures()(opts); err != nil {
		t.Errorf("EnableAlphaFeatures() = %v", err)
	}

	if !opts.AlphaFeaturesEnabled() {
		t.Errorf("AlphaFeaturesEnabled() should be true")
	}
}

func TestBetaOption(t *testing.T) {
	opts := &Options{}

	if err := EnableBetaFeatures()(opts); err != nil {
		t.Errorf("EnableBetaFeatures() = %v", err)
	}

	if !opts.BetaFeaturesEnabled() {
		t.Errorf("BetaFeaturesEnabled() should be true")
	}
}

func TestNewOptions(t *testing.T) {
	opts, err := NewOptions()

	if err != nil {
		t.Errorf("TestNewOptions() = %v", err)
	}

	if opts.AlphaFeaturesEnabled() {
		t.Errorf("AlphaFeaturesEnabled() should be false")
	}

	if opts.BetaFeaturesEnabled() {
		t.Errorf("BetaFeaturesEnabled() should be false")
	}

	opts, err = NewOptions(
		EnableAlphaFeatures(),
		EnableBetaFeatures(),
	)

	if err != nil {
		t.Errorf("TestNewOptions() = %v", err)
	}

	if !opts.AlphaFeaturesEnabled() {
		t.Errorf("AlphaFeaturesEnabled() should be true")
	}

	if !opts.BetaFeaturesEnabled() {
		t.Errorf("BetaFeaturesEnabled() should be true")
	}
}
