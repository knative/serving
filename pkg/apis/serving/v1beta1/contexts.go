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

package v1beta1

import "context"

type hdcn struct{}

func withDefaultConfigurationName(ctx context.Context) context.Context {
	return context.WithValue(ctx, hdcn{}, struct{}{})
}

func hasDefaultConfigurationName(ctx context.Context) bool {
	return ctx.Value(hdcn{}) != nil
}

type inSpec struct{}

func withinSpec(ctx context.Context) context.Context {
	return context.WithValue(ctx, inSpec{}, struct{}{})
}

func isInSpec(ctx context.Context) bool {
	return ctx.Value(inSpec{}) != nil
}

type inStatus struct{}

func withinStatus(ctx context.Context) context.Context {
	return context.WithValue(ctx, inStatus{}, struct{}{})
}

func isInStatus(ctx context.Context) bool {
	return ctx.Value(inStatus{}) != nil
}

// TODO(mattmoor): Add something to attach the name of the parent object
// to the context as a better way of validating BYO Revision name.
