//go:build integration
// +build integration

/*
Copyright 2024 The Knative Authors

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

package sharedmain

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

// TestShutdownCoordination_NormalSequence tests the normal shutdown sequence
// where queue-proxy writes drain signals and user container waits appropriately
func TestShutdownCoordination_NormalSequence(t *testing.T) {
	tmpDir := t.TempDir()
	drainDir := filepath.Join(tmpDir, "knative")
	if err := os.MkdirAll(drainDir, 0755); err != nil {
		t.Fatal(err)
	}

	drainStarted := filepath.Join(drainDir, "drain-started")
	drainComplete := filepath.Join(drainDir, "drain-complete")

	// Simulate queue-proxy PreStop
	queueProxyPreStop := func() {
		if err := os.WriteFile(drainStarted, []byte(""), 0600); err != nil {
			t.Errorf("Failed to write drain-started: %v", err)
		}
	}

	// Simulate user container PreStop with exponential backoff
	userPreStopStarted := make(chan struct{})
	userPreStopCompleted := make(chan struct{})

	go func() {
		close(userPreStopStarted)
		// Simulate the actual PreStop script logic
		for _, delay := range []int{1, 2, 4, 8} {
			if _, err := os.Stat(drainStarted); err == nil {
				// drain-started exists, wait for drain-complete
				for i := 0; i < 30; i++ { // Max 30 seconds wait
					if _, err := os.Stat(drainComplete); err == nil {
						close(userPreStopCompleted)
						return
					}
					time.Sleep(1 * time.Second)
				}
			}
			time.Sleep(time.Duration(delay) * time.Second)
		}
		// Exit after retries
		close(userPreStopCompleted)
	}()

	// Wait for user PreStop to start
	<-userPreStopStarted

	// Simulate some delay before queue-proxy writes the file
	time.Sleep(500 * time.Millisecond)

	// Execute queue-proxy PreStop
	queueProxyPreStop()

	// Simulate queue-proxy draining and writing complete signal
	time.Sleep(2 * time.Second)
	if err := os.WriteFile(drainComplete, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}

	// Wait for user PreStop to complete
	select {
	case <-userPreStopCompleted:
		// Success
	case <-time.After(20 * time.Second):
		t.Fatal("User PreStop did not complete in time")
	}

	// Verify both files exist
	if _, err := os.Stat(drainStarted); os.IsNotExist(err) {
		t.Error("drain-started file was not created")
	}
	if _, err := os.Stat(drainComplete); os.IsNotExist(err) {
		t.Error("drain-complete file was not created")
	}
}

// TestShutdownCoordination_QueueProxyCrash tests behavior when queue-proxy
// crashes or fails to write the drain-started signal
func TestShutdownCoordination_QueueProxyCrash(t *testing.T) {
	tmpDir := t.TempDir()
	drainDir := filepath.Join(tmpDir, "knative")
	if err := os.MkdirAll(drainDir, 0755); err != nil {
		t.Fatal(err)
	}

	drainStarted := filepath.Join(drainDir, "drain-started")

	// Simulate user container PreStop without queue-proxy creating the file
	userPreStopCompleted := make(chan struct{})
	startTime := time.Now()

	go func() {
		// Simulate the actual PreStop script logic
		for _, delay := range []int{1, 2, 4, 8} {
			if _, err := os.Stat(drainStarted); err == nil {
				t.Error("drain-started should not exist in crash scenario")
			}
			time.Sleep(time.Duration(delay) * time.Second)
		}
		// Should exit after retries
		close(userPreStopCompleted)
	}()

	// Wait for user PreStop to complete
	select {
	case <-userPreStopCompleted:
		elapsed := time.Since(startTime)
		// Should complete after 1+2+4+8 = 15 seconds
		if elapsed < 14*time.Second || elapsed > 16*time.Second {
			t.Errorf("PreStop took %v, expected ~15s", elapsed)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("User PreStop did not complete after retries")
	}
}

// TestShutdownCoordination_HighLoad tests shutdown under high request load
func TestShutdownCoordination_HighLoad(t *testing.T) {
	tmpDir := t.TempDir()
	drainDir := filepath.Join(tmpDir, "knative")
	if err := os.MkdirAll(drainDir, 0755); err != nil {
		t.Fatal(err)
	}

	drainStarted := filepath.Join(drainDir, "drain-started")
	drainComplete := filepath.Join(drainDir, "drain-complete")

	// Simulate active requests
	var pendingRequests int32 = 100
	var wg sync.WaitGroup

	// Simulate queue-proxy handling requests
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Simulate request processing
			time.Sleep(time.Duration(100+id*10) * time.Millisecond)
			atomic.AddInt32(&pendingRequests, -1)
		}(i)
	}

	// Start shutdown sequence
	go func() {
		// Write drain-started immediately
		if err := os.WriteFile(drainStarted, []byte(""), 0600); err != nil {
			t.Errorf("Failed to write drain-started: %v", err)
		}

		// Wait for all requests to complete
		for atomic.LoadInt32(&pendingRequests) > 0 {
			time.Sleep(100 * time.Millisecond)
		}

		// Write drain-complete after all requests are done
		if err := os.WriteFile(drainComplete, []byte(""), 0600); err != nil {
			t.Errorf("Failed to write drain-complete: %v", err)
		}
	}()

	// Simulate user container waiting
	userPreStopCompleted := make(chan struct{})
	go func() {
		for _, delay := range []int{1, 2, 4, 8} {
			if _, err := os.Stat(drainStarted); err == nil {
				// Wait for drain-complete
				for i := 0; i < 30; i++ {
					if _, err := os.Stat(drainComplete); err == nil {
						close(userPreStopCompleted)
						return
					}
					time.Sleep(1 * time.Second)
				}
			}
			time.Sleep(time.Duration(delay) * time.Second)
		}
		t.Error("User PreStop exited without seeing drain-complete")
	}()

	// Wait for all requests to complete
	wg.Wait()

	// Ensure user PreStop completes
	select {
	case <-userPreStopCompleted:
		// Verify no requests remain
		if atomic.LoadInt32(&pendingRequests) != 0 {
			t.Errorf("Requests remaining: %d", pendingRequests)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("User PreStop did not complete under load")
	}
}

// TestShutdownCoordination_FilePermissions tests behavior with file system issues
func TestShutdownCoordination_FilePermissions(t *testing.T) {
	// Skip if not running as root (can't test permission issues properly)
	if os.Geteuid() == 0 {
		t.Skip("Cannot test permission issues as root")
	}

	tmpDir := t.TempDir()
	drainDir := filepath.Join(tmpDir, "knative")
	if err := os.MkdirAll(drainDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Make directory read-only
	if err := os.Chmod(drainDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(drainDir, 0755) // Restore for cleanup

	drainStarted := filepath.Join(drainDir, "drain-started")

	// Try to write drain-started (should fail)
	err := os.WriteFile(drainStarted, []byte(""), 0600)
	if err == nil {
		t.Error("Expected write to fail with read-only directory")
	}

	// User PreStop should still complete after retries
	userPreStopCompleted := make(chan struct{})
	go func() {
		for _, delay := range []int{1, 2, 4, 8} {
			if _, err := os.Stat(drainStarted); err == nil {
				t.Error("File should not exist with permission issues")
			}
			time.Sleep(time.Duration(delay) * time.Millisecond) // Use ms for faster test
		}
		close(userPreStopCompleted)
	}()

	select {
	case <-userPreStopCompleted:
		// Success - PreStop completed despite permission issues
	case <-time.After(5 * time.Second):
		t.Fatal("User PreStop did not complete with permission issues")
	}
}

// TestShutdownCoordination_RaceCondition tests for race conditions
// between queue-proxy and user container PreStop hooks
func TestShutdownCoordination_RaceCondition(t *testing.T) {
	tmpDir := t.TempDir()
	drainDir := filepath.Join(tmpDir, "knative")

	// Run multiple iterations to catch race conditions
	for i := 0; i < 50; i++ {
		// Clean up from previous iteration
		os.RemoveAll(drainDir)
		if err := os.MkdirAll(drainDir, 0755); err != nil {
			t.Fatal(err)
		}

		drainStarted := filepath.Join(drainDir, "drain-started")
		drainComplete := filepath.Join(drainDir, "drain-complete")

		var wg sync.WaitGroup
		wg.Add(2)

		// Queue-proxy PreStop and shutdown
		go func() {
			defer wg.Done()
			// Random delay to create race conditions
			time.Sleep(time.Duration(i%10) * time.Millisecond)
			os.WriteFile(drainStarted, []byte(""), 0600)
			time.Sleep(time.Duration(i%5) * time.Millisecond)
			os.WriteFile(drainComplete, []byte(""), 0600)
		}()

		// User container PreStop
		completed := make(chan bool, 1)
		go func() {
			defer wg.Done()
			timeout := time.After(20 * time.Second)
			for _, delay := range []int{1, 2, 4, 8} {
				select {
				case <-timeout:
					completed <- false
					return
				default:
				}

				if _, err := os.Stat(drainStarted); err == nil {
					// Wait for complete
					for j := 0; j < 30; j++ {
						if _, err := os.Stat(drainComplete); err == nil {
							completed <- true
							return
						}
						time.Sleep(100 * time.Millisecond)
					}
				}
				time.Sleep(time.Duration(delay) * time.Millisecond)
			}
			completed <- true // Exit after retries
		}()

		// Wait for both to complete
		wg.Wait()

		if !<-completed {
			t.Errorf("Iteration %d: User PreStop timed out", i)
		}
	}
}

// TestShutdownCoordination_LongRunningRequests tests behavior with
// requests that take longer than the grace period
func TestShutdownCoordination_LongRunningRequests(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()

	tmpDir := t.TempDir()
	drainDir := filepath.Join(tmpDir, "knative")
	if err := os.MkdirAll(drainDir, 0755); err != nil {
		t.Fatal(err)
	}

	drainStarted := filepath.Join(drainDir, "drain-started")
	drainComplete := filepath.Join(drainDir, "drain-complete")

	// Simulate a long-running request
	requestComplete := make(chan struct{})
	go func() {
		logger.Info("Starting long-running request")
		time.Sleep(10 * time.Second) // Longer than typical drain timeout
		close(requestComplete)
		logger.Info("Long-running request completed")
	}()

	// Start shutdown
	go func() {
		os.WriteFile(drainStarted, []byte(""), 0600)

		// In real scenario, this would wait for requests or timeout
		select {
		case <-requestComplete:
			logger.Info("Request completed, writing drain-complete")
		case <-time.After(5 * time.Second):
			logger.Info("Timeout waiting for request, writing drain-complete anyway")
		}

		os.WriteFile(drainComplete, []byte(""), 0600)
	}()

	// User container should still proceed
	userExited := make(chan struct{})
	go func() {
		for _, delay := range []int{1, 2, 4, 8} {
			if _, err := os.Stat(drainStarted); err == nil {
				// Wait for complete with timeout
				for i := 0; i < 10; i++ {
					if _, err := os.Stat(drainComplete); err == nil {
						close(userExited)
						return
					}
					time.Sleep(1 * time.Second)
				}
			}
			time.Sleep(time.Duration(delay) * time.Second)
		}
		close(userExited)
	}()

	select {
	case <-userExited:
		// User container should exit even with long-running request
	case <-time.After(30 * time.Second):
		t.Fatal("User container did not exit with long-running request")
	}
}
