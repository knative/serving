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

func (se *suiteExecution) processOperationGroup(op operationGroup) {
	se.configuration.T.Run(op.groupName, func(t *testing.T) {
		l, err := se.configuration.logger(t)
		if err != nil {
			t.Fatal(err)
		}
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
					l, err = se.configuration.logger(t)
					if err != nil {
						t.Fatal(err)
					}
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
	operations := []func(num int){
		se.installingBase,
		se.preUpgradeTests,
	}
	for _, operation := range operations {
		operation(idx)
		idx++
		if se.failed {
			return
		}
	}

	se.startContinualTests(idx)
	idx++
	if se.failed {
		return
	}
	defer func() {
		se.verifyContinualTests(idx)
	}()

	operations = []func(num int){
		se.upgradeWith,
		se.postUpgradeTests,
		se.downgradeWith,
		se.postDowngradeTests,
	}
	for _, operation := range operations {
		operation(idx)
		idx++
		if se.failed {
			return
		}
	}
}
