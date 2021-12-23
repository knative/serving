/*
Copyright 2021 The Knative Authors

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

package helpers

import "context"

type (
	revCtxKey struct{}
)

type revCtx struct {
	anns map[string]string
}

// WithAnnotations attaches the Revision annotation to the context.
func WithAnnotations(ctx context.Context, anns map[string]string) context.Context {
	return context.WithValue(ctx, revCtxKey{}, &revCtx{
		anns: anns,
	})
}

// AnnotationsFrom retrieves the Revision annotations array from the context.
func AnnotationsFrom(ctx context.Context) map[string]string {
	if ctx.Value(revCtxKey{}) != nil {
		return ctx.Value(revCtxKey{}).(*revCtx).anns
	}
	return nil
}
