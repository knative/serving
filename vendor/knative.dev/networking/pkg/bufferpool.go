/*
Copyright 2020 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package pkg

import (
	"net/http/httputil"
	"sync"
)

// bufferPool implements the BufferPool interface to be used in httputil.ReverseProxy.
//
// Sadly, because of the httputil.BufferPool interface, we cannot avoid an allocation
// when returning the slice to the pool. We could choose to not use use pointers of
// slices and use straight slices instead, but that'd cause an allocation likewise
// because of the interface{} API. It'd also trigger a staticcheck warning.
// See https://staticcheck.io/docs/checks#SA6002.
type bufferPool struct {
	pool *sync.Pool
}

// NewBufferPool creates a new BufferPool. This is only safe to use in the context
// of a httputil.ReverseProxy, as the buffers returned via Put are not cleaned
// explicitly.
func NewBufferPool() httputil.BufferPool {
	return &bufferPool{
		// We don't use the New function of sync.Pool here to avoid an unnecessary
		// allocation when creating the slices. They are implicitly created in the
		// Get function below.
		pool: &sync.Pool{},
	}
}

// Get gets a []byte from the bufferPool, or creates a new one if none are
// available in the pool.
func (b *bufferPool) Get() []byte {
	buf := b.pool.Get()
	if buf == nil {
		// Use the default buffer size as defined in the ReverseProxy itself.
		return make([]byte, 32*1024)
	}

	return *buf.(*[]byte)
}

// Put returns the given Buffer to the bufferPool.
func (b *bufferPool) Put(buffer []byte) {
	b.pool.Put(&buffer)
}
