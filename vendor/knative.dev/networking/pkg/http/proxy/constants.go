/*
Copyright 2022 The Knative Authors

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

package proxy

const (

	// FlushInterval controls the time when we flush the connection in the
	// reverse proxies (Activator, QP).
	// As of go1.16, a FlushInterval of 0 (the default) still flushes immediately
	// when Content-Length is -1, which means the default works properly for
	// streaming/websockets, without flushing more often than necessary for
	// non-streaming requests.
	FlushInterval = 0
)
