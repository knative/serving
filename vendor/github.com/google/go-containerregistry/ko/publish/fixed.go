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

package publish

import (
	"fmt"

	"github.com/google/go-containerregistry/name"
	"github.com/google/go-containerregistry/v1"
)

type fixed struct {
	base    name.Repository
	entries map[string]v1.Hash
}

// NewFixed returns a publish.Interface implementation that simply resolves particular
// references to fixed name.Digest references.
func NewFixed(base name.Repository, entries map[string]v1.Hash) Interface {
	return &fixed{base, entries}
}

// Publish implements publish.Interface
func (f *fixed) Publish(_ v1.Image, s string) (name.Reference, error) {
	h, ok := f.entries[s]
	if !ok {
		return nil, fmt.Errorf("unsupported importpath: %q", s)
	}
	d, err := name.NewDigest(fmt.Sprintf("%s/%s@%s", f.base, s, h), name.WeakValidation)
	if err != nil {
		return nil, err
	}
	return &d, nil
}
