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
	"time"

	"go.uber.org/zap"
)

// Suite represents a upgrade tests suite that can be executed and will perform
// execution in predictable manner.
type Suite struct {
	Tests         Tests
	Installations Installations
}

// Tests holds a list of operations for various part of upgrade suite.
type Tests struct {
	PreUpgrade    []Operation
	PostUpgrade   []Operation
	PostDowngrade []Operation
	Continual     []BackgroundOperation
}

// Installations holds a list of operations that will install Knative components
// in different versions.
type Installations struct {
	Base          []Operation
	UpgradeWith   []Operation
	DowngradeWith []Operation
}

// Operation represents a upgrade test operation like test or installation that
// can be provided by specific component or reused in aggregating components.
type Operation interface {
	// Name is a human readable operation title, and it will be used in t.Run.
	Name() string
	// Handler is a function that will be called to perform an operation.
	Handler() func(c Context)
}

// BackgroundOperation represents a upgrade test operation that will be
// performed in background while other operations is running. To achieve that
// a passed BackgroundContext should be used to synchronize it's operations with
// Ready and Stop channels.
type BackgroundOperation interface {
	// Name is a human readable operation title, and it will be used in t.Run.
	Name() string
	// Setup method may be used to set up environment before upgrade/downgrade is
	// performed.
	Setup() func(c Context)
	// Handler will be executed in background while upgrade/downgrade is being
	// executed. It can be used to constantly validate environment during that
	// time and/or wait for StopEvent being sent. After StopEvent is received
	// user should validate environment, clean up resources, and report found
	// issues to testing.T forwarded in StepEvent.
	Handler() func(bc BackgroundContext)
}

// Context is an object that is passed to every operation. It contains testing.T
// for error reporting and zap.SugaredLogger for unbuffered logging.
type Context struct {
	T   *testing.T
	Log *zap.SugaredLogger
}

// BackgroundContext is a upgrade test execution context that will be passed
// down to each handler of BackgroundOperation. It contains a stop event channel.
// Until stop event is sent user may use zap.SugaredLogger to log state of execution if
// necessary.
type BackgroundContext struct {
	T    *testing.T
	Log  *zap.SugaredLogger
	Stop <-chan struct{}
}

// WaitForStopEventConfiguration holds a values to be used be WaitForStopEvent
// function. OnStop will be called when a stop event is sent. OnWait will be
// invoked in a loop while waiting, and each wait act is driven by WaitTime
// amount.
type WaitForStopEventConfiguration struct {
	Name     string
	OnStop   func()
	OnWait   func(bc BackgroundContext, self WaitForStopEventConfiguration)
	WaitTime time.Duration
}

// Configuration holds required and optional configuration to run upgrade tests.
type Configuration struct {
	T *testing.T
	LogConfig
}

// LogConfig holds the logger configuration. It allows for passing just the
// logger configuration options.
type LogConfig struct {
	// Config from which the zap.Logger be created.
	// Deprecated: This config doesn't have effect. Use Options instead.
	Config zap.Config
	// Options holds options for the zap.Logger.
	Options []zap.Option
}

// SuiteExecutor is to execute upgrade test suite.
type SuiteExecutor interface {
	Execute(c Configuration)
}
