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
	revCtxKey        struct{}
	reporterCtxKey   struct{}
	healthyTargetKey struct{}
)

type revCtx struct {
	revision *v1.Revision
	revID    types.NamespacedName
}

// WithRevisionAndID attaches the Revision and the ID to the context.
func WithRevisionAndID(ctx context.Context, rev *v1.Revision, revID types.NamespacedName) context.Context {
	return context.WithValue(ctx, revCtxKey{}, &revCtx{
		revision: rev,
		revID:    revID,
	})
}

// WithReporterContext attaches a precomputed metrics reporter context to the request context.
func WithReporterContext(ctx context.Context, reporterCtx context.Context) context.Context {
	if reporterCtx == nil {
		return ctx
	}
	return context.WithValue(ctx, reporterCtxKey{}, reporterCtx)
}

// ReporterContextFrom retrieves the metrics reporter context from the request context if present.
func ReporterContextFrom(ctx context.Context) context.Context {
	v := ctx.Value(reporterCtxKey{})
	if v == nil {
		return nil
	}
	return v.(context.Context)
}

// WithHealthyTarget marks the request context as targeting a healthy backend.
func WithHealthyTarget(ctx context.Context, healthy bool) context.Context {
	if !healthy {
		return ctx
	}
	return context.WithValue(ctx, healthyTargetKey{}, true)
}

// IsHealthyTarget returns whether the request context is marked as targeting a healthy backend.
func IsHealthyTarget(ctx context.Context) bool {
	v := ctx.Value(healthyTargetKey{})
	b, _ := v.(bool)
	return b
}

// RevisionFrom retrieves the Revision object from the context.
func RevisionFrom(ctx context.Context) *v1.Revision {
	return ctx.Value(revCtxKey{}).(*revCtx).revision
}

// RevIDFrom retrieves the revisionID from the context.
func RevIDFrom(ctx context.Context) types.NamespacedName {
	return ctx.Value(revCtxKey{}).(*revCtx).revID
}

func RevAnnotation(ctx context.Context, annotation string) string {
	v := ctx.Value(revCtxKey{})
	if v == nil {
		return ""
	}
	rev := v.(*revCtx).revision
	if rev != nil && rev.GetAnnotations() != nil {
		return rev.GetAnnotations()[annotation]
	}
	return ""
}
