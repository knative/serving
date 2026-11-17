/*
Copyright 2026 The Knative Authors

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

package statforwarder

import (
	"context"
	"testing"
	"testing/synctest"

	"go.uber.org/zap"
	"knative.dev/pkg/logging"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
)

func TestCancelWithFullRetryQueue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := logging.WithLogger(context.Background(), zap.NewNop().Sugar())
		f := New(ctx, testBs)

		// With no processor, the consumer schedules a retry and returns to
		// waiting for stats. Wait leaves the retry asleep on the fake clock.
		f.Process(stat1)
		synctest.Wait()

		cancelled := make(chan struct{})
		go func() {
			f.Cancel()
			close(cancelled)
		}()
		<-f.stopCh
		synctest.Wait()

		// The consumer has exited and is waiting for the sleeping retry.
		// Fill the queue directly to model concurrent sends that fill it
		// after the consumer stops, independently of Process's stop check.
		for range cap(f.statCh) {
			f.statCh <- stat{sm: stat1}
		}

		// The fake clock advances to wake the retry. It must abandon its
		// send rather than block forever on the full, undrained queue.
		<-cancelled
		if got, want := len(f.statCh), cap(f.statCh); got != want {
			t.Errorf("queue length = %d, want %d", got, want)
		}
	})
}

func TestCancelUnblocksProcess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		ctx := logging.WithLogger(context.Background(), logger)
		f := New(ctx, testBs)
		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		f.setProcessor(bucket1, &localProcessor{
			logger: logger,
			accept: func(asmetrics.StatMessage) {
				select {
				case entered <- struct{}{}:
				default:
				}
				<-release
			},
		})

		f.Process(stat1)
		<-entered
		for range cap(f.statCh) {
			f.Process(stat1)
		}
		processed := make(chan struct{})
		go func() {
			f.Process(stat1)
			close(processed)
		}()
		synctest.Wait()
		select {
		case <-processed:
			t.Error("Process returned before queue space or cancellation")
		default:
		}

		cancelled := make(chan struct{})
		go func() {
			f.Cancel()
			close(cancelled)
		}()
		<-f.stopCh
		synctest.Wait()
		select {
		case <-processed:
		default:
			t.Error("cancellation did not unblock Process")
		}
		select {
		case <-cancelled:
			t.Error("Cancel returned before in-flight processing completed")
		default:
		}

		close(release)
		<-cancelled
	})
}

func TestProcessAfterCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := logging.WithLogger(context.Background(), zap.NewNop().Sugar())
		f := New(ctx, testBs)
		f.Cancel()
		for range 10 {
			f.Process(stat1)
		}
		if got := len(f.statCh); got != 0 {
			t.Errorf("Process enqueued %d stats after cancellation", got)
		}
	})
}

func TestConcurrentCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := zap.NewNop().Sugar()
		ctx := logging.WithLogger(context.Background(), logger)
		f := New(ctx, testBs)
		entered := make(chan struct{})
		release := make(chan struct{})
		finished := make(chan struct{})
		f.setProcessor(bucket1, &localProcessor{
			logger: logger,
			accept: func(asmetrics.StatMessage) {
				close(entered)
				<-release
				close(finished)
			},
		})
		f.Process(stat1)
		<-entered

		const callers = 8
		cancelled := make(chan struct{}, callers)
		cancel := func() {
			f.Cancel()
			select {
			case <-finished:
			default:
				t.Error("Cancel returned before in-flight processing completed")
			}
			cancelled <- struct{}{}
		}
		go cancel()
		<-f.stopCh
		synctest.Wait()
		for range callers - 1 {
			go cancel()
		}
		close(release)
		for range callers {
			<-cancelled
		}
		f.Cancel()
	})
}
