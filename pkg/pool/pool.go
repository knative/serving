/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package pool

import (
	"context"
	"sync"
)

type impl struct {
	wg     sync.WaitGroup
	workCh chan func() error

	// Ensure that we Wait exactly once and memoize
	// the result.
	waitOnce sync.Once

	cancel context.CancelFunc

	// We're only interested in the first result so
	// only set it once.
	resultOnce sync.Once
	result     error
}

// impl implements Interface
var _ Interface = (*impl)(nil)

// defaultCapacity is the number of work items or errors that we
// can queue up before calls to Go will block, or work will
// block until Wait is called.
const defaultCapacity = 50

// New creates a fresh worker pool with the specified size.
func New(workers int) Interface {
	return NewWithCapacity(workers, defaultCapacity)
}

// NewWithCapacity creates a fresh worker pool with the specified size.
func NewWithCapacity(workers, capacity int) Interface {
	i, _ := NewWithContext(context.Background(), workers, capacity)
	return i
}

// NewWithContext creates a pool that is driven by a cancelable context.
// Just like errgroup.Group on first error the context will be canceled as well.
func NewWithContext(ctx context.Context, workers, capacity int) (Interface, context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	i := &impl{
		cancel: cancel,
		workCh: make(chan func() error, capacity),
	}

	// Start a go routine for each worker, which:
	// 1. reads off of the work channel,
	// 2. (optionally) sets the error as the result,
	// 3. marks work as done in our sync.WaitGroup.
	for idx := 0; idx < workers; idx++ {
		go func() {
			for work := range i.workCh {
				i.exec(work)
			}
		}()
	}
	return i, ctx
}

func (i *impl) exec(w func() error) {
	defer i.wg.Done()
	if err := w(); err != nil {
		i.resultOnce.Do(func() {
			if i.cancel != nil {
				i.cancel()
			}
			i.result = err
		})
	}
}

// Go implements Interface.
func (i *impl) Go(w func() error) {
	// Increment the amount of outstanding work we're waiting on.
	i.wg.Add(1)
	// Send the work along the queue.
	i.workCh <- w
}

// Wait implements Interface.
func (i *impl) Wait() error {
	i.waitOnce.Do(func() {
		// Wait for queued work to complete.
		i.wg.Wait()
		// Notify the context, that it's done now.
		if i.cancel != nil {
			i.cancel()
		}

		// Now we know there are definitely no new items arriving.
		close(i.workCh)
	})

	return i.result
}
