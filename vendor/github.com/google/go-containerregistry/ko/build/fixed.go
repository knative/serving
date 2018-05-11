// Copyright 2018 Google LLC All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package build

import (
	"fmt"

	"github.com/google/go-containerregistry/v1"
)

type fixed struct {
	entries map[string]v1.Image
}

// NewFixed returns a build.Interface implementation that simply resolves particular
// references to fixed v1.Image objects
func NewFixed(entries map[string]v1.Image) Interface {
	return &fixed{entries}
}

// IsSupportedReference implements build.Interface
func (f *fixed) IsSupportedReference(s string) bool {
	_, ok := f.entries[s]
	return ok
}

// Build implements build.Interface
func (f *fixed) Build(s string) (v1.Image, error) {
	if img, ok := f.entries[s]; ok {
		return img, nil
	}
	return nil, fmt.Errorf("unsupported reference: %q", s)
}
