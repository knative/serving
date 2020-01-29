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

package apis

import "context"

// ConvertUpViaProxy attempts to convert a specific source to a sink
// through a proxy
func ConvertUpViaProxy(
	ctx context.Context,
	source, proxy, sink Convertible,
) error {

	if err := source.ConvertUp(ctx, proxy); err != nil {
		return err
	}

	return proxy.ConvertUp(ctx, sink)
}

// ConvertDownViaProxy attempts to convert a specific sink from a source
// through a proxy
func ConvertDownViaProxy(
	ctx context.Context,
	source, proxy, sink Convertible,
) error {

	if err := proxy.ConvertDown(ctx, source); err != nil {
		return err
	}

	return sink.ConvertDown(ctx, proxy)
}
