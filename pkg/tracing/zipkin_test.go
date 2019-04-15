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

package tracing

import (
	"testing"

	"github.com/knative/serving/pkg/tracing/config"
)

func TestCreateReporter(t *testing.T) {
	rep, err := CreateZipkinReporter(&config.Config{
		ZipkinEndpoint: "http://localhost:8000",
	})
	if err != nil {
		t.Errorf("Failed to create reporter: %v", err)
	}

	err = rep.Close()
	if err != nil {
		t.Errorf("Failed to close reporter: %v", err)
	}

	_, err = CreateZipkinReporter(&config.Config{})
	if err != nil {
		t.Errorf("Failed to create reporter: %v", err)
	}
}
