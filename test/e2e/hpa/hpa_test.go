// +build e2e hpa

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

package hpa

import (
	"testing"

	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/test/e2e"
)

func TestAutoscaleUpCountPodsHPA(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	e2e.RunAutoscaleUpCountPods(t, autoscaling.HPA, autoscaling.Concurrency)
}

func TestRPSBasedAutoscaleUpCountPodsHPA(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	e2e.RunAutoscaleUpCountPods(t, autoscaling.HPA, autoscaling.RPS)
}
