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

package testing

import (
	configmaptesting "github.com/knative/pkg/configmap/testing"
	logtesting "github.com/knative/pkg/logging/testing"
	"github.com/knative/pkg/reconciler/testing"
)

type (
	TableTest          = testing.TableTest
	TableRow           = testing.TableRow
	ActionRecorderList = testing.ActionRecorderList
	ActionRecorder     = testing.ActionRecorder
	EventList          = testing.EventList
	Factory            = testing.Factory
	HookResult         = testing.HookResult
	FakeStatsReporter  = testing.FakeStatsReporter
	FakeClock          = testing.FakeClock
	NullTracker        = testing.NullTracker
)

var (
	InduceFailure             = testing.InduceFailure
	KeyOrDie                  = testing.KeyOrDie
	NewHooks                  = testing.NewHooks
	ExpectNormalEventDelivery = testing.ExpectNormalEventDelivery
	ValidateCreates           = testing.ValidateCreates
	ValidateUpdates           = testing.ValidateUpdates
	ConfigMapFromTestFile     = configmaptesting.ConfigMapFromTestFile
	ConfigMapsFromTestFile    = configmaptesting.ConfigMapsFromTestFile
	Eventf                    = testing.Eventf

	PrependGenerateNameReactor = testing.PrependGenerateNameReactor

	TestLogger = logtesting.TestLogger

	// ClearAllLoggers removes all the registered test loggers.
	ClearAllLoggers = logtesting.ClearAll
)

const (
	HookComplete   = testing.HookComplete
	HookIncomplete = testing.HookIncomplete
)
