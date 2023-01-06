/*
Copyright 2020 The Knative Authors

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

package upgrade

type suiteExecution struct {
	suite         *enrichedSuite
	configuration Configuration
}

type enrichedSuite struct {
	installations Installations
	tests         enrichedTests
}

type enrichedTests struct {
	preUpgrade    []Operation
	postUpgrade   []Operation
	postDowngrade []Operation
	continual     []stoppableOperation
}

type stoppableOperation struct {
	BackgroundOperation
	stop chan struct{}
}

type operationGroup struct {
	num                   int
	operations            []Operation
	groupName             string
	groupTemplate         string
	elementTemplate       string
	skippingGroupTemplate string
}

type simpleOperation struct {
	name    string
	handler func(c Context)
}

type simpleBackgroundOperation struct {
	name    string
	setup   func(c Context)
	handler func(bc BackgroundContext)
}
