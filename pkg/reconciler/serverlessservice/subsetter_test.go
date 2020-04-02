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

package serverlessservice

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestBuildHashes(t *testing.T) {
	const target = "a target"
	set := []string{"a", "b", "c"}

	s1, st1, f1 := buildHashes(set, target)
	s2, st2, f2 := buildHashes(set, target)
	t.Logf("Start = %x, Step = %x, From = %x", s1, st1, f1)

	res := []struct {
		Start uint64
		Step  uint64
		From  []uint64
	}{{
		s1, st1, f1,
	}, {
		s2, st2, f2,
	}}
	// Verify it's consistent from run to run.
	if !cmp.Equal(res[1], res[0]) {
		t.Errorf("Resutls are not consistent, diff(-want,+got):\n%s", cmp.Diff(res[0], res[1]))
	}
}
