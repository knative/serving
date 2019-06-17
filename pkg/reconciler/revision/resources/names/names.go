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

package names

import (
	"crypto/md5"
	"fmt"

	"github.com/knative/pkg/kmeta"
)

const (
	// K8s restriction for the resource name.
	longest = 63
	md5Len  = 32
	head    = longest - md5Len
)

func maybeShortenName(n string) string {
	if len(n) > longest {
		n = fmt.Sprintf("%s%x", n[:head], md5.Sum([]byte(n)))
	}
	return n
}

// Deployment returns the precomputed name for the revision deployment
func Deployment(rev kmeta.Accessor) string {
	return maybeShortenName(rev.GetName() + "-deployment")
}

// ImageCache returns the precomputed name for the image cache.
func ImageCache(rev kmeta.Accessor) string {
	return maybeShortenName(rev.GetName() + "-cache")
}

// KPA returns the PA name for the revision.
func KPA(rev kmeta.Accessor) string {
	// We want the KPA's "key" to match the revision,
	// to simplify the transition to the KPA.
	return rev.GetName()
}
