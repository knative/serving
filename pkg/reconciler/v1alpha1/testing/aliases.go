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
	logtesting "github.com/knative/pkg/logging/testing"
	"github.com/knative/serving/pkg/reconciler/testing"
)

type (
	TableTest          = testing.TableTest
	TableRow           = testing.TableRow
	ActionRecorderList = testing.ActionRecorderList
	ActionRecorder     = testing.ActionRecorder
	Factory            = testing.Factory
	HookResult         = testing.HookResult
	PhaseTest          = testing.PhaseTest
	PhaseTests         = testing.PhaseTests
	ReconcilerTest     = testing.ReconcilerTest
	ReconcilerTests    = testing.ReconcilerTests
	Creates            = testing.Creates
	Patches            = testing.Patches
	Failures           = testing.Failures
	Objects            = testing.Objects
	Updates            = testing.Updates
	FakeClient         = testing.FakeClient
	NullTracker        = testing.NullTracker

	FakeConfigMapWatcher = testing.FakeConfigMapWatcher
	FakeConfigStore      = testing.FakeConfigStore
	FakeWorkQueue        = testing.FakeWorkQueue
)

var (
	InduceFailure             = testing.InduceFailure
	KeyOrDie                  = testing.KeyOrDie
	NewHooks                  = testing.NewHooks
	ExpectNormalEventDelivery = testing.ExpectNormalEventDelivery
	ValidateCreates           = testing.ValidateCreates
	ValidateUpdates           = testing.ValidateUpdates
	ConfigMapFromTestFile     = testing.ConfigMapFromTestFile

	TestLogger            = logtesting.TestLogger
	TestContextWithLogger = logtesting.TestContextWithLogger
)

const (
	HookComplete = testing.HookComplete
)
