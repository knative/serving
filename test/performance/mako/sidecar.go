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

	"github.com/golang/protobuf/proto"
	"github.com/google/mako/helpers/go/quickstore"
	qpb "github.com/google/mako/helpers/proto/quickstore/quickstore_go_proto"
	"knative.dev/pkg/changeset"
)

const (
	// sidecarAddress is the address of the Mako sidecar to which we locally
	// write results, and it authenticates and publishes them to Mako after
	// assorted preprocessing.
	sidecarAddress = "localhost:9813"
)

// SetupMako sets up the mako client for the provided benchmarkKey.
// It will add a few common tags and allows each benchmark to add custm tags as well.
// It returns the mako client handle to sotre metrics, a method to close the connection
// to mako server once done and error if case of failures.
func Setup(ctx context.Context, benchmarkKey *string, extraTags ...string) (*quickstore.Quickstore, func(context.Context), error) {
	var close func(context.Context)
	tags := make([]string, 0)
	if commitID, err := changeset.Get(); err == nil {
		tags = append(tags, commitID)
	} else {
		return nil, close, err
	}

	tags = append(tags, extraTags...)
	return quickstore.NewAtAddress(ctx, &qpb.QuickstoreInput{
		BenchmarkKey: proto.String(*benchmarkKey),
		Tags:         tags,
	}, sidecarAddress)
}
