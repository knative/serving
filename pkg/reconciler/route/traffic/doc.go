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

/*
This package contains the following:
- The types and functions that translate our TrafficTarget to an
	intermediate format that is used to construct Kingress.
	In particular it deals with flattening the TrafficTarget to the
	revision level -- it also does the grouping of traffic target
	into their traffic groups.
- The types and functions that deal with latest revision rollout.
	The types encapsulate the rollout state and its serialization.
	The Rollout type is a state machine that migrates the traffic
	from source revisions to the latest target revision.
*/

package traffic
