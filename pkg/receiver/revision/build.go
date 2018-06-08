/*
Copyright 2018 Google LLC.

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
	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
)

// SyncBuild implements build.Receiver
func (c *Receiver) SyncBuild(build *buildv1alpha1.Build) error {
	cond := getBuildDoneCondition(build)
	if cond == nil {
		// The build isn't done, so ignore this event.
		return nil
	}

	// For each of the revisions watching this build, mark their build phase as complete.
	for k := range c.buildtracker.GetTrackers(build) {
		// Look up the revision to mark complete.
		namespace, name := splitKey(k)
		rev, err := c.lister.Revisions(namespace).Get(name)
		if err != nil {
			return err
		}
		if err := c.markBuildComplete(rev, cond); err != nil {
			return err
		}
	}

	return nil
}
