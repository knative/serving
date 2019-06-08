/*
Copyright 2018 The Knative Authors

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

package reconciler

import (
	"context"
	"testing"

	_ "github.com/knative/caching/pkg/client/injection/client/fake"
	_ "github.com/knative/pkg/client/injection/client/fake"
	_ "github.com/knative/pkg/injection/clients/dynamicclient/fake"
	_ "github.com/knative/pkg/injection/clients/kubeclient/fake"
	_ "github.com/knative/serving/pkg/client/injection/client/fake"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/injection"
	logtesting "github.com/knative/pkg/logging/testing"
	_ "github.com/knative/pkg/system/testing"
	"k8s.io/client-go/rest"
)

var reconcilerName = "test-reconciler"

func TestNew(t *testing.T) {
	defer logtesting.ClearAll()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := &rest.Config{}
	ctx, _ = injection.Fake.SetupInformers(ctx, cfg)

	cmw := configmap.NewFixedWatcher()

	r := NewBase(ctx, reconcilerName, cmw)

	if r == nil {
		t.Fatal("Expected NewBase to return a non-nil value")
	}
	if r.Recorder == nil {
		t.Fatal("Expected NewBase to add a Recorder")
	}
	if r.StatsReporter == nil {
		t.Fatal("Expected NewBase to add a StatsReporter")
	}
}
