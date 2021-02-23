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

/*
This file contains the context manipulation routines used within
the activator binary.
*/

package handler

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

type (
	revisionKey struct{}
	revIDKey    struct{}
)

// WithRevision attaches the Revision object to the context.
func WithRevision(ctx context.Context, rev *v1.Revision) context.Context {
	return context.WithValue(ctx, revisionKey{}, rev)
}

// RevisionFrom retrieves the Revision object from the context.
func RevisionFrom(ctx context.Context) *v1.Revision {
	return ctx.Value(revisionKey{}).(*v1.Revision)
}

// WithRevID attaches the the revisionID to the context.
func WithRevID(ctx context.Context, revID types.NamespacedName) context.Context {
	return context.WithValue(ctx, revIDKey{}, revID)
}

// RevIDFrom retrieves the the revisionID from the context.
func RevIDFrom(ctx context.Context) types.NamespacedName {
	return ctx.Value(revIDKey{}).(types.NamespacedName)
}
