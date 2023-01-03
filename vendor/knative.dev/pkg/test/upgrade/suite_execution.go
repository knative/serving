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

func (se *suiteExecution) processOperationGroup(t *testing.T, op operationGroup) {
	l := se.logger
	t.Run(op.groupName, func(t *testing.T) {
		if len(op.operations) > 0 {
			l.Infof(op.groupTemplate, op.num, len(op.operations))
			for i, operation := range op.operations {
				l.Infof(op.elementTemplate, op.num, i+1, operation.Name())
				if se.failed {
					l.Debugf(skippingOperationTemplate, operation.Name())
					return
				}
				handler := operation.Handler()
				t.Run(operation.Name(), func(t *testing.T) {
					handler(Context{T: t, Log: l})
				})
				se.failed = se.failed || t.Failed()
				if se.failed {
					return
				}
			}
		} else {
			l.Infof(op.skippingGroupTemplate, op.num)
		}
	})
}

func (se *suiteExecution) execute() {
	idx := 1
	stopCh := make(chan struct{})
	operations := []func(t *testing.T, num int){
		se.installingBase,
		se.preUpgradeTests,
	}
	for _, operation := range operations {
		operation(se.configuration.T, idx)
		idx++
		if se.failed {
			return
		}
	}

	upgradesExecuted := false
	se.configuration.T.Run("Parallel", func(t *testing.T) {
		// Calls t.Parallel() after doing setup phase. The second part runs in parallel
		// with UpgradeDowngrade test below.
		se.runContinualTests(t, idx, stopCh)

		// Make sure the stop channel is closed and continual tests unblocked in the event
		// of failing continual tests (possibly calling t.Fatal) when the rest of sub-tests
		// are skipped.
		defer func() {
			if !upgradesExecuted {
				close(stopCh)
			}
		}()

		idx++
		if se.failed {
			return
		}

		operations = []func(t *testing.T, num int){
			se.upgradeWith,
			se.postUpgradeTests,
			se.downgradeWith,
			se.postDowngradeTests,
		}
		t.Run("UpgradeDowngrade", func(t *testing.T) {
			upgradesExecuted = true
			defer close(stopCh)
			// The rest of this test group will run in parallel with individual continual tests.
			t.Parallel()
			for _, operation := range operations {
				operation(t, idx)
				idx++
				if se.failed {
					return
				}
			}
		})
	})
}
