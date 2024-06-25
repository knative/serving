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
	"go.uber.org/zap/zaptest"
)

// Execute the Suite of upgrade tests with a Configuration given.
// When the suite includes Continual tests the number of logical CPUs
// usable by the test process must be at least <NUMBER OF CONTINUAL TESTS> + 1.
// The -parallel test flag or GOMAXPROCS environment variable might be
// used to adjust the settings.
func (s *Suite) Execute(c Configuration) {
	l := c.logger(c.T)
	se := suiteExecution{
		suite:         enrichSuite(s),
		configuration: c,
	}
	l.Info("🏃 Running upgrade test suite...")

	se.execute()

	if !c.T.Failed() {
		l.Info("🥳🎉 Success! Upgrade suite completed without errors.")
	} else {
		l.Error("💣🤬💔️ Upgrade suite have failed!")
	}
}

func (c Configuration) logger(t *testing.T) *zap.SugaredLogger {
	return zaptest.NewLogger(t, zaptest.WrapOptions(c.Options...)).Sugar()
}

// NewOperation creates a new upgrade operation or test.
func NewOperation(name string, handler func(c Context)) Operation {
	return &simpleOperation{name: name, handler: handler}
}

// NewBackgroundVerification is convenience function to easily setup a
// background operation that will setup environment and then verify environment
// status after receiving a stop event.
func NewBackgroundVerification(name string, setup func(c Context), verify func(c Context)) BackgroundOperation {
	return NewBackgroundOperation(name, setup, func(bc BackgroundContext) {
		WaitForStopEvent(bc, WaitForStopEventConfiguration{
			Name: name,
			OnStop: func() {
				verify(Context{
					T:   bc.T,
					Log: bc.Log,
				})
			},
			OnWait:   DefaultOnWait,
			WaitTime: DefaultWaitTime,
		})
	})
}

// NewBackgroundOperation creates a new background operation or test that can be
// notified to stop its operation.
func NewBackgroundOperation(name string, setup func(c Context),
	handler func(bc BackgroundContext),
) BackgroundOperation {
	return &simpleBackgroundOperation{
		name:    name,
		setup:   setup,
		handler: handler,
	}
}

// WaitForStopEvent will wait until upgrade suite sends a stop event to it.
// After that happen a handler is invoked to verify environment state and report
// failures.
func WaitForStopEvent(bc BackgroundContext, w WaitForStopEventConfiguration) {
	for {
		select {
		case <-bc.Stop:
			bc.Log.Debugf("%s received a stop event", w.Name)
			w.OnStop()
			return
		default:
			w.OnWait(bc, w)
		}
		time.Sleep(w.WaitTime)
	}
}

func enrichSuite(s *Suite) *enrichedSuite {
	es := &enrichedSuite{
		installations: s.Installations,
		tests: enrichedTests{
			preUpgrade:    s.Tests.PreUpgrade,
			postUpgrade:   s.Tests.PostUpgrade,
			postDowngrade: s.Tests.PostDowngrade,
			continual:     make([]stoppableOperation, len(s.Tests.Continual)),
		},
	}
	for i, test := range s.Tests.Continual {
		es.tests.continual[i] = stoppableOperation{
			BackgroundOperation: test,
			stop:                make(chan struct{}),
		}
	}
	return es
}

// Name is a human readable operation title, and it will be used in t.Run.
func (h *simpleOperation) Name() string {
	return h.name
}

// Handler is a function that will be called to perform an operation.
func (h *simpleOperation) Handler() func(c Context) {
	return h.handler
}

// Name is a human readable operation title, and it will be used in t.Run.
func (s *simpleBackgroundOperation) Name() string {
	return s.name
}

// Setup method may be used to set up environment before upgrade/downgrade is
// performed.
func (s *simpleBackgroundOperation) Setup() func(c Context) {
	return s.setup
}

// Handler will be executed in background while upgrade/downgrade is being
// executed. It can be used to constantly validate environment during that
// time and/or wait for a stop event being sent. After a stop event is received
// user should validate environment, clean up resources, and report found
// issues to testing.T forwarded in BackgroundContext.
func (s *simpleBackgroundOperation) Handler() func(bc BackgroundContext) {
	return s.handler
}
