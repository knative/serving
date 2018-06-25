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

package revision

import (
	"fmt"
	"sync"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

type key string

func getKey(namespace, name string) key {
	return key(fmt.Sprintf("%s/%s", namespace, name))
}

type set map[key]struct{}

func (s set) add(k key) {
	s[k] = struct{}{}
}

func (s set) remove(k key) {
	delete(s, k)
}

func (s set) has(k key) bool {
	_, ok := s[k]
	return ok
}

type buildTracker struct {
	// buildMtx guards modifications to builds
	buildMtx sync.Mutex
	// The collection of outstanding builds and the sets of revisions waiting for them.
	builds map[key]set
}

func (bt *buildTracker) Track(u *v1alpha1.Revision) bool {
	bt.buildMtx.Lock()
	defer bt.buildMtx.Unlock()

	// When this build is complete, mark this revision as ready.
	bk := getKey(u.Namespace, u.Spec.BuildName)
	entry, ok := bt.builds[bk]
	if !ok {
		entry = set{}
	}
	k := getKey(u.Namespace, u.Name)
	if _, ok := entry[k]; ok {
		// Already tracked.
		return true
	}

	entry.add(k)
	bt.builds[bk] = entry

	return false
}

func (bt *buildTracker) Untrack(u *v1alpha1.Revision) {
	bt.buildMtx.Lock()
	defer bt.buildMtx.Unlock()

	// When this build is complete, mark this revision as ready.
	bk := getKey(u.Namespace, u.Spec.BuildName)
	entry, ok := bt.builds[bk]
	if ok {
		entry.remove(getKey(u.Namespace, u.Name))
		if len(entry) == 0 {
			delete(bt.builds, bk)
		} else {
			bt.builds[bk] = entry
		}
	}
}

func (bt *buildTracker) GetTrackers(build *buildv1alpha1.Build) set {
	bt.buildMtx.Lock()
	defer bt.buildMtx.Unlock()

	s, ok := bt.builds[getKey(build.Namespace, build.Name)]
	if !ok {
		// Nothing is watching this build, so ignore this event.
		return set{}
	}

	return s
}
