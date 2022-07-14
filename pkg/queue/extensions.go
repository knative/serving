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

package queue

import (
	"context"
	"net/http"

	"go.uber.org/zap"
)

// QPExtension is the interface for interacting with queue proxy injection
// implementations. Extending this interface will allow adding more
// QP extendiability.
type QPExtension interface {
	Init(ctx context.Context, logger *zap.SugaredLogger)
	Shutdown()

	// If extension does not require to be added to Transport
	// (e.g. when the extensoin is not active),
	// Transport should return next (never return nil)
	Transport(next http.RoundTripper) (roundTripper http.RoundTripper)
}
