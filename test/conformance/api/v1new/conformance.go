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

package v1new

import (
	"flag"
	"testing"

	"knative.dev/reconciler-test/pkg/test"
	"knative.dev/reconciler-test/pkg/test/environment"

	// Needed to hit GCP clusters
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

// TODO - A subset of this would exist in knative.dev/serving/test
type ConformanceT struct {
	test.T

	// Target namespace where our tests create resources
	Namespace string

	// // TODO - use this vs. SYSTEM_NAMESPACE env-var
	// SystemNamespace string

	Cluster environment.Cluster
	Images  environment.Images

	ServingClients *ServingClients
}

func (c *ConformanceT) AddFlags(fs *flag.FlagSet) {
	c.T.AddFlags(fs)
	c.Cluster.AddFlags(fs)
	c.Images.AddFlags(fs)
}

func (c *ConformanceT) Setup(t *testing.T) {
	if c.Namespace == "" {
		// TODO(dprotaso) - use constant when we promote this
		c.Namespace = "serving-tests"
	}

	// TODO - re-enable once we can pass it a custom namespace
	// cancel := logstream.Start(t)
	// t.Cleanup(cancel)

	cfg := c.Cluster.ClientConfig()

	// We poll, so set our limits high.
	cfg.QPS = 100
	cfg.Burst = 200

	var err error
	if c.ServingClients, err = newServingClients(cfg, c.Namespace); err != nil {
		t.Fatal(err)
	}
}

func (c *ConformanceT) CleanupResources(names *ResourceNames) {
	// TODO - clean up on signals
	c.Cleanup(func() {
		c.ServingClients.Delete(
			[]string{names.Route},
			[]string{names.Config},
			[]string{names.Service},
		)
	})
}
