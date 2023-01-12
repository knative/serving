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

import (
	"testing"
)

const skippingOperationTemplate = `Skipping "%s" as previous operation have failed`

func (se *suiteExecution) installingBase(t *testing.T, num int) {
	se.processOperationGroup(t, operationGroup{
		num:                   num,
		operations:            se.suite.installations.Base,
		groupName:             "InstallingBase",
		elementTemplate:       `%d.%d) Installing base install of "%s".`,
		skippingGroupTemplate: "%d) 💿 No base installation registered. Skipping.",
		groupTemplate:         "%d) 💿 Installing base installations. %d are registered.",
	})
}

func (se *suiteExecution) preUpgradeTests(t *testing.T, num int) {
	se.processOperationGroup(t, operationGroup{
		num:                   num,
		operations:            se.suite.tests.preUpgrade,
		groupName:             "PreUpgradeTests",
		elementTemplate:       `%d.%d) Testing with "%s".`,
		skippingGroupTemplate: "%d) ✅️️ No pre upgrade tests registered. Skipping.",
		groupTemplate: "%d) ✅️️ Testing functionality before upgrade is performed." +
			" %d tests are registered.",
	})
}

func (se *suiteExecution) runContinualTests(t *testing.T, num int, stopCh <-chan struct{}) {
	l := se.configuration.logger(t)
	operations := se.suite.tests.continual
	groupTemplate := "%d) 🔄 Starting continual tests. " +
		"%d tests are registered."
	elementTemplate := `%d.%d) Starting continual tests of "%s".`
	numOps := len(operations)
	if numOps > 0 {
		l.Infof(groupTemplate, num, numOps)
		for i := range operations {
			operation := operations[i]
			l.Debugf(elementTemplate, num, i+1, operation.Name())
			t.Run(operation.Name(), func(t *testing.T) {
				l := se.configuration.logger(t)
				setup := operation.Setup()
				setup(Context{T: t, Log: l})
				if t.Failed() {
					return
				}
				t.Parallel()
				handle := operation.Handler()
				// Blocking operation.
				handle(BackgroundContext{
					T:    t,
					Log:  l,
					Stop: stopCh,
				})
				l.Debugf(`Finished "%s"`, operation.Name())
			})
		}
	} else {
		l.Infof("%d) 🔄 No continual tests registered. Skipping.", num)
	}
}

func (se *suiteExecution) upgradeWith(t *testing.T, num int) {
	se.processOperationGroup(t, operationGroup{
		num:                   num,
		operations:            se.suite.installations.UpgradeWith,
		groupName:             "UpgradeWith",
		elementTemplate:       `%d.%d) Upgrading with "%s".`,
		skippingGroupTemplate: "%d) 📀 No upgrade operations registered. Skipping.",
		groupTemplate:         "%d) 📀 Upgrading with %d registered operations.",
	})
}

func (se *suiteExecution) postUpgradeTests(t *testing.T, num int) {
	se.processOperationGroup(t, operationGroup{
		num:                   num,
		operations:            se.suite.tests.postUpgrade,
		groupName:             "PostUpgradeTests",
		elementTemplate:       `%d.%d) Testing with "%s".`,
		skippingGroupTemplate: "%d) ✅️️ No post upgrade tests registered. Skipping.",
		groupTemplate: "%d) ✅️️ Testing functionality after upgrade is performed." +
			" %d tests are registered.",
	})
}

func (se *suiteExecution) downgradeWith(t *testing.T, num int) {
	se.processOperationGroup(t, operationGroup{
		num:                   num,
		operations:            se.suite.installations.DowngradeWith,
		groupName:             "DowngradeWith",
		elementTemplate:       `%d.%d) Downgrading with "%s".`,
		skippingGroupTemplate: "%d) 💿 No downgrade operations registered. Skipping.",
		groupTemplate:         "%d) 💿 Downgrading with %d registered operations.",
	})
}

func (se *suiteExecution) postDowngradeTests(t *testing.T, num int) {
	se.processOperationGroup(t, operationGroup{
		num:                   num,
		operations:            se.suite.tests.postDowngrade,
		groupName:             "PostDowngradeTests",
		elementTemplate:       `%d.%d) Testing with "%s".`,
		skippingGroupTemplate: "%d) ✅️️ No post downgrade tests registered. Skipping.",
		groupTemplate: "%d) ✅️️ Testing functionality after downgrade is performed." +
			" %d tests are registered.",
	})
}
