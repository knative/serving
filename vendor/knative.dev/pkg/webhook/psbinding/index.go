/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless requ ired by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package psbinding

import "k8s.io/apimachinery/pkg/labels"

// exactKey is the type for keys that match exactly.
type exactKey struct {
	Group     string
	Kind      string
	Namespace string
	Name      string
}

// exactMatcher is our reverse index from subjects to the Bindings that apply to
// them.
type exactMatcher map[exactKey]Bindable

// Add writes a key into the reverse index.
func (em exactMatcher) Add(key exactKey, b Bindable) {
	em[key] = b
}

// Get fetches the key from the reverse index, if present.
func (em exactMatcher) Get(key exactKey) (bindable Bindable, present bool) {
	b, ok := em[key]
	return b, ok
}

// inexactKey is the type for keys that match inexactly (via selector)
type inexactKey struct {
	Group     string
	Kind      string
	Namespace string
}

// pair holds selectors and bindables for a particular inexactKey.
type pair struct {
	selector labels.Selector
	sb       Bindable
}

// inexactMatcher is our reverse index from subjects to the Bindings that apply to
// them.
type inexactMatcher map[inexactKey][]pair

// Add writes a key into the reverse index.
func (im inexactMatcher) Add(key inexactKey, selector labels.Selector, b Bindable) {
	pl := im[key]
	pl = append(pl, pair{
		selector: selector,
		sb:       b,
	})
	im[key] = pl
}

// Get fetches the key from the reverse index, if present.
func (im inexactMatcher) Get(key inexactKey, ls labels.Set) (bindable Bindable, present bool) {
	// Iterate over the list of pairs matched for this GK + namespace and return the first
	// Bindable that matches our selector.
	for _, p := range im[key] {
		if p.selector.Matches(ls) {
			return p.sb, true
		}
	}
	return nil, false
}
