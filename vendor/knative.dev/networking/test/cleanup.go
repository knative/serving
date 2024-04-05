/*
Copyright 2018 The Knative Authors

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

// cleanup allows you to define a cleanup function that will be executed
// if your test is interrupted.

package test

import (
	"os"
	"os/signal"
	"sync"
	"testing"
)

func init() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go cleanupOnInterrupt(c)
}

var cf struct {
	o sync.Once
	m sync.RWMutex
	f []func()
}

// cleanupOnInterrupt registers a signal handler and will execute a stack of functions if an interrupt signal is caught
func cleanupOnInterrupt(c chan os.Signal) {
	for range c {
		cf.o.Do(func() {
			cf.m.RLock()
			defer cf.m.RUnlock()
			for i := len(cf.f) - 1; i >= 0; i-- {
				cf.f[i]()
			}
			os.Exit(1)
		})
	}
}

// CleanupOnInterrupt stores cleanup functions to execute if an interrupt signal is caught
func CleanupOnInterrupt(cleanup func()) {
	cf.m.Lock()
	defer cf.m.Unlock()
	cf.f = append(cf.f, cleanup)
}

// EnsureCleanup will run the provided cleanup function when the test ends,
// either via t.Cleanup or on interrupt via CleanupOnInterrupt.
func EnsureCleanup(t *testing.T, cleanup func()) {
	t.Cleanup(cleanup)
	CleanupOnInterrupt(cleanup)
}
