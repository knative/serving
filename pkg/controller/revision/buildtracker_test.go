/*
Copyright 2018 The Knative Authors.

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
	"testing"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSet(t *testing.T) {
	s := &set{}
	key1 := getKey("foo", "bar")
	key2 := getKey("baz", "blah")

	// Not added yet
	if s.has(key1) {
		t.Errorf("s.has(%v) = true, want false", key1)
	}

	// Add it.
	s.add(key1)
	if !s.has(key1) {
		t.Errorf("s.has(%v) = false, want true", key1)
	}

	// Add it redundantly.
	s.add(key1)
	if !s.has(key1) {
		t.Errorf("s.has(%v) = false, want true", key1)
	}

	// Add another key
	s.add(key2)
	if !s.has(key2) {
		t.Errorf("s.has(%v) = false, want true", key2)
	}

	// Remove it.
	s.remove(key1)
	if s.has(key1) {
		t.Errorf("s.has(%v) = true, want false", key1)
	}
	if !s.has(key2) {
		t.Errorf("s.has(%v) = false, want true", key2)
	}

	// Remove it redundantly.
	s.remove(key1)
	if s.has(key1) {
		t.Errorf("s.has(%v) = true, want false", key1)
	}
	if !s.has(key2) {
		t.Errorf("s.has(%v) = false, want true", key2)
	}

	// Remove the other key.
	s.remove(key2)
	if s.has(key1) {
		t.Errorf("s.has(%v) = true, want false", key1)
	}
	if s.has(key2) {
		t.Errorf("s.has(%v) = true, want false", key2)
	}
}

func TestTrack(t *testing.T) {
	bt := &buildTracker{builds: make(map[key]set)}

	build := &buildv1alpha1.Build{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
			Name:      "bar",
		},
	}
	rev1 := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
			Name:      "baz",
		},
		Spec: v1alpha1.RevisionSpec{
			BuildName: "bar",
		},
	}
	key1 := getKey(rev1.Namespace, rev1.Name)
	rev2 := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
			Name:      "blah",
		},
		Spec: v1alpha1.RevisionSpec{
			BuildName: "bar",
		},
	}
	key2 := getKey(rev2.Namespace, rev2.Name)

	// Before any tracking, neither key shows up.
	trackers := bt.GetTrackers(build)
	if trackers.has(key1) {
		t.Errorf("trackers.has(%v) = true, want false", key1)
	}
	if trackers.has(key2) {
		t.Errorf("trackers.has(%v) = true, want false", key2)
	}

	// Tracking it for rev1 should make key1 show up
	bt.Track(rev1)
	trackers = bt.GetTrackers(build)
	if !trackers.has(key1) {
		t.Errorf("trackers.has(%v) = false, want true", key1)
	}
	if trackers.has(key2) {
		t.Errorf("trackers.has(%v) = true, want false", key2)
	}

	// Tracking it for rev2 should make key2 show up
	bt.Track(rev2)
	trackers = bt.GetTrackers(build)
	if !trackers.has(key1) {
		t.Errorf("trackers.has(%v) = false, want true", key1)
	}
	if !trackers.has(key2) {
		t.Errorf("trackers.has(%v) = false, want true", key2)
	}

	// Tracking it for rev1 AGAIN should be a noop
	bt.Track(rev1)
	trackers = bt.GetTrackers(build)
	if !trackers.has(key1) {
		t.Errorf("trackers.has(%v) = false, want true", key1)
	}
	if !trackers.has(key2) {
		t.Errorf("trackers.has(%v) = false, want true", key2)
	}

	// Untracking rev1 should make key1 go away.
	bt.Untrack(rev1)
	trackers = bt.GetTrackers(build)
	if trackers.has(key1) {
		t.Errorf("trackers.has(%v) = true, want false", key1)
	}
	if !trackers.has(key2) {
		t.Errorf("trackers.has(%v) = false, want true", key2)
	}

	// Untracking rev2 should make key2 go away.
	bt.Untrack(rev2)
	trackers = bt.GetTrackers(build)
	if trackers.has(key1) {
		t.Errorf("trackers.has(%v) = true, want false", key1)
	}
	if trackers.has(key2) {
		t.Errorf("trackers.has(%v) = true, want false", key2)
	}

	// Untracking rev2 AGAIN should be a noop
	bt.Untrack(rev2)
	trackers = bt.GetTrackers(build)
	if trackers.has(key1) {
		t.Errorf("trackers.has(%v) = true, want false", key1)
	}
	if trackers.has(key2) {
		t.Errorf("trackers.has(%v) = true, want false", key2)
	}
}
