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

package mako

import (
	"context"
	"runtime"
	"strings"

	"knative.dev/pkg/injection/clients/kubeclient"

	"github.com/google/mako/helpers/go/quickstore"
	qpb "github.com/google/mako/helpers/proto/quickstore/quickstore_go_proto"
	"k8s.io/client-go/rest"
	"knative.dev/pkg/changeset"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/injection"
)

const (
	// sidecarAddress is the address of the Mako sidecar to which we locally
	// write results, and it authenticates and publishes them to Mako after
	// assorted preprocessing.
	sidecarAddress = "localhost:9813"
)

// EscapeTag replaces characters that Mako doesn't accept with ones it does.
func EscapeTag(tag string) string {
	return strings.ReplaceAll(tag, ".", "_")
}

// Setup sets up the mako client for the provided benchmarkKey.
// It will add a few common tags and allows each benchmark to add custm tags as well.
// It returns the mako client handle to sotre metrics, a method to close the connection
// to mako server once done and error if case of failures.
func Setup(ctx context.Context, extraTags ...string) (context.Context, *quickstore.Quickstore, func(context.Context), error) {
	tags := append(MustGetTags(), extraTags...)
	// Get the commit of the benchmarks
	commitID, err := changeset.Get()
	if err != nil {
		return nil, nil, nil, err
	}

	// Setup a deployment informer, so that we can use the lister to track
	// desired and available pod counts.
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, informers := injection.Default.SetupInformers(ctx, cfg)
	if err := controller.StartInformers(ctx.Done(), informers...); err != nil {
		return nil, nil, nil, err
	}

	// Get the Kubernetes version from the API server.
	version, err := kubeclient.Get(ctx).Discovery().ServerVersion()
	if err != nil {
		return nil, nil, nil, err
	}

	qs, qclose, err := quickstore.NewAtAddress(ctx, &qpb.QuickstoreInput{
		BenchmarkKey: MustGetBenchmark(),
		Tags: append(tags,
			"commit="+commitID,
			"kubernetes="+EscapeTag(version.String()),
			EscapeTag(runtime.Version()),
		),
	}, sidecarAddress)
	if err != nil {
		return nil, nil, nil, err
	}
	return ctx, qs, qclose, nil
}
