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
	t.Run(op.groupName, func(t *testing.T) {
		l := se.configuration.logger(t)
		if len(op.operations) > 0 {
			l.Infof(op.groupTemplate, op.num, len(op.operations))
			for i, operation := range op.operations {
				l.Infof(op.elementTemplate, op.num, i+1, operation.Name())
				if t.Failed() {
					l.Debugf(skippingOperationTemplate, operation.Name())
					return
				}
				handler := operation.Handler()
				t.Run(operation.Name(), func(t *testing.T) {
					handler(Context{T: t, Log: l})
				})
				if t.Failed() {
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
	t := se.configuration.T
	operations := []func(t *testing.T, num int){
		se.installingBase,
		se.preUpgradeTests,
	}
	for _, operation := range operations {
		operation(t, idx)
		idx++
		if t.Failed() {
			return
		}
	}

	t.Run("Run", func(t *testing.T) {
		// Calls t.Parallel() after doing setup phase. The second part runs in parallel
		// with Steps below.
		se.runContinualTests(t, idx, stopCh)

		idx++
		// At this point only the setup phase of continual tests was done. We want
		// to quit early in the event of failures.
		if t.Failed() {
			close(stopCh)
			return
		}

		operations = []func(t *testing.T, num int){
			se.upgradeWith,
			se.postUpgradeTests,
			se.downgradeWith,
			se.postDowngradeTests,
		}
		t.Run("Steps", func(t *testing.T) {
			defer close(stopCh)
			// The rest of this test group will run in parallel with individual continual tests.
			t.Parallel()
			for _, operation := range operations {
				operation(t, idx)
				idx++
				if t.Failed() {
					return
				}
			}
		})
	})
}
