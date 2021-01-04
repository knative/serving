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

func (se *suiteExecution) installingBase(num int) {
	se.processOperationGroup(operationGroup{
		num:                   num,
		operations:            se.suite.installations.Base,
		groupName:             "InstallingBase",
		elementTemplate:       `%d.%d) Installing base install of "%s".`,
		skippingGroupTemplate: "%d) 💿 No base installation registered. Skipping.",
		groupTemplate:         "%d) 💿 Installing base installations. %d are registered.",
	})
}

func (se *suiteExecution) preUpgradeTests(num int) {
	se.processOperationGroup(operationGroup{
		num:                   num,
		operations:            se.suite.tests.preUpgrade,
		groupName:             "PreUpgradeTests",
		elementTemplate:       `%d.%d) Testing with "%s".`,
		skippingGroupTemplate: "%d) ✅️️ No pre upgrade tests registered. Skipping.",
		groupTemplate: "%d) ✅️️ Testing functionality before upgrade is performed." +
			" %d tests are registered.",
	})
}

func (se *suiteExecution) startContinualTests(num int) {
	l := se.logger
	operations := se.suite.tests.continual
	groupTemplate := "%d) 🔄 Starting continual tests. " +
		"%d tests are registered."
	elementTemplate := `%d.%d) Starting continual tests of "%s".`
	numOps := len(operations)
	se.configuration.T.Run("ContinualTests", func(t *testing.T) {
		if numOps > 0 {
			l.Infof(groupTemplate, num, numOps)
			for i := range operations {
				operation := operations[i]
				l.Infof(elementTemplate, num, i+1, operation.Name())
				if se.failed {
					l.Debugf(skippingOperationTemplate, operation.Name())
					return
				}
				setup := operation.Setup()
				t.Run("Setup"+operation.Name(), func(t *testing.T) {
					setup(Context{T: t, Log: l})
				})
				handler := operation.Handler()
				go func() {
					bc := BackgroundContext{Log: l, Stop: operation.stop}
					handler(bc)
				}()

				se.failed = se.failed || t.Failed()
				if se.failed {
					return
				}
			}

		} else {
			l.Infof("%d) 🔄 No continual tests registered. Skipping.", num)
		}
	})
}

func (se *suiteExecution) verifyContinualTests(num int) {
	l := se.logger
	testsCount := len(se.suite.tests.continual)
	if testsCount > 0 {
		se.configuration.T.Run("VerifyContinualTests", func(t *testing.T) {
			l.Infof("%d) ✋ Verifying %d running continual tests.", num, testsCount)
			for i, operation := range se.suite.tests.continual {
				t.Run(operation.Name(), func(t *testing.T) {
					l.Infof(`%d.%d) Verifying "%s".`, num, i+1, operation.Name())
					finished := make(chan struct{})
					operation.stop <- StopEvent{
						T:        t,
						Finished: finished,
						name:     "Stop of " + operation.Name(),
					}
					<-finished
					se.failed = se.failed || t.Failed()
					l.Debugf(`Finished "%s"`, operation.Name())
				})
			}
		})
	}
}

func (se *suiteExecution) upgradeWith(num int) {
	se.processOperationGroup(operationGroup{
		num:                   num,
		operations:            se.suite.installations.UpgradeWith,
		groupName:             "UpgradeWith",
		elementTemplate:       `%d.%d) Upgrading with "%s".`,
		skippingGroupTemplate: "%d) 📀 No upgrade operations registered. Skipping.",
		groupTemplate:         "%d) 📀 Upgrading with %d registered operations.",
	})
}

func (se *suiteExecution) postUpgradeTests(num int) {
	se.processOperationGroup(operationGroup{
		num:                   num,
		operations:            se.suite.tests.postUpgrade,
		groupName:             "PostUpgradeTests",
		elementTemplate:       `%d.%d) Testing with "%s".`,
		skippingGroupTemplate: "%d) ✅️️ No post upgrade tests registered. Skipping.",
		groupTemplate: "%d) ✅️️ Testing functionality after upgrade is performed." +
			" %d tests are registered.",
	})
}

func (se *suiteExecution) downgradeWith(num int) {
	se.processOperationGroup(operationGroup{
		num:                   num,
		operations:            se.suite.installations.DowngradeWith,
		groupName:             "DowngradeWith",
		elementTemplate:       `%d.%d) Downgrading with "%s".`,
		skippingGroupTemplate: "%d) 💿 No downgrade operations registered. Skipping.",
		groupTemplate:         "%d) 💿 Downgrading with %d registered operations.",
	})
}

func (se *suiteExecution) postDowngradeTests(num int) {
	se.processOperationGroup(operationGroup{
		num:                   num,
		operations:            se.suite.tests.postDowngrade,
		groupName:             "PostDowngradeTests",
		elementTemplate:       `%d.%d) Testing with "%s".`,
		skippingGroupTemplate: "%d) ✅️️ No post downgrade tests registered. Skipping.",
		groupTemplate: "%d) ✅️️ Testing functionality after downgrade is performed." +
			" %d tests are registered.",
	})
}
