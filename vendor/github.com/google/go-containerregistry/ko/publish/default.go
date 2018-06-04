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
	"log"
	"net/http"

	"github.com/google/go-containerregistry/authn"
	"github.com/google/go-containerregistry/name"
	"github.com/google/go-containerregistry/v1"
	"github.com/google/go-containerregistry/v1/remote"
)

// defalt is intentionally misspelled to avoid keyword collision (and drive Jon nuts).
type defalt struct {
	base name.Repository
	t    http.RoundTripper
	wo   remote.WriteOptions
}

// NewDefault returns a new publish.Interface that publishes references under the provided base
// repository using the default keychain to authenticate and the default naming scheme.
func NewDefault(base name.Repository, t http.RoundTripper, wo remote.WriteOptions) Interface {
	return &defalt{base, t, wo}
}

// Publish implements publish.Interface
func (d *defalt) Publish(img v1.Image, s string) (name.Reference, error) {
	auth, err := authn.DefaultKeychain.Resolve(d.base.Registry)
	if err != nil {
		return nil, err
	}
	// We push via tag (always latest) and then produce a digest because some registries do
	// not support publishing by digest.
	tag, err := name.NewTag(fmt.Sprintf("%s/%s:latest", d.base, s), name.WeakValidation)
	if err != nil {
		return nil, err
	}
	log.Printf("Publishing %v", tag)
	if err := remote.Write(tag, img, auth, d.t, d.wo); err != nil {
		return nil, err
	}
	h, err := img.Digest()
	if err != nil {
		return nil, err
	}
	dig, err := name.NewDigest(fmt.Sprintf("%s/%s@%s", d.base, s, h), name.WeakValidation)
	if err != nil {
		return nil, err
	}
	log.Printf("Published %v", dig)
	return &dig, nil
}
