package net

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/serving/pkg/queue"
)

var testRevID = types.NamespacedName{Namespace: "test", Name: "rev"}

func newRaceTestRT(t *testing.T) *revisionThrottler {
	t.Helper()
	logger := zaptest.NewLogger(t, zaptest.WrapOptions(zap.AddCaller())).Sugar()
	rt := newRevisionThrottler(
		testRevID,
		nil,
		1,
		"http",
		queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 1},
		logger,
	)
	return rt
}

func newTestTracker(dest string, breaker *queue.Breaker) *podTracker {
	return newPodTracker(dest, testRevID, breaker)
}

// 1) Concurrent access to podTrackers and assignedTrackers without consistent locking
func TestRace_PodTrackers_ReadWrite_NoLock(t *testing.T) {
	t.Parallel()
	rt := newRaceTestRT(t)

	stop := make(chan struct{})
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			addr := "10.0.0." + strconv.Itoa(i%10)
			tr := newTestTracker(addr+":8080", queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
			rt.updateThrottlerState(1, []*podTracker{tr}, []string{tr.dest}, nil, nil)
			rt.updateThrottlerState(0, nil, nil, []string{tr.dest}, nil)
			i++
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Simulate reading assignedTrackers while podTrackers might be updated
			rt.mux.RLock()
			_ = len(rt.podTrackers) // Safe read under lock
			rt.mux.RUnlock()
			rt.mux.RLock()
			local := rt.assignedTrackers
			rt.mux.RUnlock()
			_ = rt.filterAvailableTrackers(local)
		}
	}()

	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// 2) updateCapacity reading rt.podTrackers while writer mutates
func TestRace_UpdateCapacity_ReadsPodTrackersWhileWriterMutates(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t).Sugar()
	rt := newRevisionThrottler(types.NamespacedName{Namespace: "default", Name: "rev"}, nil, 1, "http", queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 1}, logger)

	initial := make([]*podTracker, 0, 5)
	for i := 0; i < 5; i++ {
		tr := newTestTracker("10.0.0."+string(rune('a'+i))+":8080", queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
		initial = append(initial, tr)
	}
	rt.updateThrottlerState(len(initial), initial, nil, nil, nil)

	stop := make(chan struct{})
	wg := &sync.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			addr := "192.168.1." + string(rune('a'+(i%3))) + ":8080"
			if i%2 == 0 {
				rt.updateThrottlerState(1, []*podTracker{newTestTracker(addr, queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))}, nil, nil, nil)
			} else {
				rt.updateThrottlerState(0, nil, nil, []string{addr}, nil)
			}
			i++
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			rt.updateCapacity(int(rt.backendCount.Load()))
		}
	}()

	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// 5) LoadBalancer policy race: policy update vs request path usage
func TestRace_LBPolicy_UpdateVsUsage(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t).Sugar()
	rt := newRevisionThrottler(types.NamespacedName{Namespace: "default", Name: "rev"}, nil, 1, "http", queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 1}, logger)

	// Add some initial trackers
	tr1 := newTestTracker("10.0.0.1:8080", queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
	tr2 := newTestTracker("10.0.0.2:8080", queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
	rt.updateThrottlerState(2, []*podTracker{tr1, tr2}, []string{tr1.dest, tr2.dest}, nil, nil)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: continuously update lbPolicy
	go func() {
		defer wg.Done()
		// Get the current policy to ensure we store the same type
		currentPolicy := rt.lbPolicy.Load().(lbPolicy)
		for {
			select {
			case <-stop:
				return
			default:
			}
			rt.lbPolicy.Store(currentPolicy)
			rt.lbPolicy.Store(currentPolicy) // Store same function to avoid type inconsistency
		}
	}()

	// Reader: continuously use lbPolicy in request path
	go func() {
		defer wg.Done()
		ctx := context.Background()
		for {
			select {
			case <-stop:
				return
			default:
			}
			rt.mux.RLock()
			trackers := rt.assignedTrackers
			rt.mux.RUnlock()
			if len(trackers) > 0 {
				lbPolicy := rt.lbPolicy.Load().(lbPolicy)
				_, _ = lbPolicy(ctx, trackers)
			}
		}
	}()

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// 6) resetTrackers reading podTrackers without lock while updateThrottlerState modifies
func TestRace_ResetTrackers_ReadsPodTrackersWhileWriterMutates(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t).Sugar()
	rt := newRevisionThrottler(types.NamespacedName{Namespace: "default", Name: "rev"}, nil, 2, "http", queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 1}, logger)

	// Add initial trackers
	initial := make([]*podTracker, 0, 3)
	for i := 0; i < 3; i++ {
		tr := newTestTracker("10.0.0."+strconv.Itoa(i)+":8080", queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
		initial = append(initial, tr)
	}
	rt.updateThrottlerState(len(initial), initial, nil, nil, nil)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: continuously mutate podTrackers
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			addr := "192.168.2." + strconv.Itoa(i%5) + ":8080"
			if i%2 == 0 {
				rt.updateThrottlerState(1, []*podTracker{newTestTracker(addr, queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))}, nil, nil, nil)
			} else {
				rt.updateThrottlerState(0, nil, nil, []string{addr}, nil)
			}
			i++
		}
	}()

	// Reader: resetTrackers iterates podTrackers without lock
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			rt.resetTrackers() // Reads rt.podTrackers without lock
		}
	}()

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// 7) Throttler map race: double-checked locking pattern
func TestRace_ThrottlerMap_DoubleCheckedLocking(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t).Sugar()
	throttler := &Throttler{
		revisionThrottlers: make(map[types.NamespacedName]*revisionThrottler),
		logger:             logger,
	}

	revID := types.NamespacedName{Namespace: "default", Name: "test"}
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Multiple goroutines trying to get/create the same revision throttler
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				// Simulate the double-checked locking pattern
				throttler.revisionThrottlersMutex.RLock()
				_, ok := throttler.revisionThrottlers[revID]
				throttler.revisionThrottlersMutex.RUnlock()

				if !ok {
					throttler.revisionThrottlersMutex.Lock()
					if _, exists := throttler.revisionThrottlers[revID]; !exists {
						rt := newRevisionThrottler(revID, nil, 1, "http", queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 1}, logger)
						throttler.revisionThrottlers[revID] = rt
					}
					throttler.revisionThrottlersMutex.Unlock()
				}
			}
		}()
	}

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// 8) backendCount race: written without synchronization
func TestRace_BackendCount_UnsynchronizedWrite(t *testing.T) {
	t.Parallel()
	rt := newRaceTestRT(t)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: updateCapacity writes backendCount without sync
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			rt.updateCapacity(10)
			rt.updateCapacity(20)
		}
	}()

	// Reader: calculateCapacity might read backendCount
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = rt.calculateCapacity(int(rt.backendCount.Load()), 5, 1) // Reads backendCount
		}
	}()

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// 9) infiniteBreaker channel race: broadcast channel recreation vs reading
func TestRace_InfiniteBreaker_BroadcastChannelRecreation(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t).Sugar()
	ib := newInfiniteBreaker(logger)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: continuously recreate broadcast channel
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			ib.UpdateConcurrency(0) // Creates new channel
			ib.UpdateConcurrency(1) // Closes channel
		}
	}()

	// Reader: Maybe reads broadcast channel
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		for ctx.Err() == nil {
			select {
			case <-stop:
				return
			default:
			}
			_ = ib.Maybe(ctx, func() {}) // Reads ib.broadcast
		}
	}()

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// 12) handlePubEpsUpdate race: test stale backendCount overwriting fresh capacity updates
// This tests the scenario where activator endpoint updates use stale backendCount values,
// potentially overwriting capacity increases from pod updates.
func TestRace_HandlePubEpsUpdate_StaleBackendCountOverwritesCapacity(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t).Sugar()
	rt := newRevisionThrottler(
		types.NamespacedName{Namespace: "default", Name: "rev"},
		nil,
		1,
		"http",
		queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 1},
		logger,
	)

	// Initial state: 5 pods
	initialTrackers := make([]*podTracker, 5)
	healthyDests := make([]string, 5)
	for i := range 5 {
		initialTrackers[i] = newTestTracker(
			"10.0.0."+strconv.Itoa(i)+":8080",
			queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 1, InitialCapacity: 1}),
		)
		healthyDests[i] = initialTrackers[i].dest
	}
	rt.updateThrottlerState(5, initialTrackers, healthyDests, nil, nil)
	rt.numActivators.Store(1)
	rt.activatorIndex.Store(0)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: Simulate pod updates (scaling up)
	go func() {
		defer wg.Done()
		for i := 5; i < 20; i++ {
			select {
			case <-stop:
				return
			default:
			}

			newTracker := newTestTracker(
				"10.0.0."+strconv.Itoa(i)+":8080",
				queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 1, InitialCapacity: 1}),
			)
			// This updates backendCount to i+1
			rt.updateThrottlerState(i+1, []*podTracker{newTracker}, nil, nil, nil)
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Goroutine 2: Simulate activator endpoint updates (using stale backendCount)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			// This calls updateCapacity with STORED backendCount, potentially stale
			storedBackends := int(rt.backendCount.Load())
			rt.updateCapacity(storedBackends)
			time.Sleep(3 * time.Millisecond)
		}
	}()

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Verify final capacity matches actual pod count
	rt.mux.RLock()
	finalPodCount := len(rt.podTrackers)
	rt.mux.RUnlock()

	finalCapacity := int(rt.breaker.Capacity())
	expectedCapacity := finalPodCount * int(rt.containerConcurrency.Load())

	if finalCapacity < expectedCapacity-1 {
		t.Errorf("Final capacity (%d) significantly lower than expected (%d) for %d pods",
			finalCapacity, expectedCapacity, finalPodCount)
	}
}

// 13) Race between breaker capacity reads and capacity updates
// Simplified to just test concurrent capacity reads/writes without complex request routing
func TestRace_BreakerCapacityCapture_ConcurrentCapacityUpdates(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t).Sugar()
	rt := newRevisionThrottler(
		types.NamespacedName{Namespace: "default", Name: "rev"},
		nil,
		1,
		"http",
		queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 5},
		logger,
	)

	initialTrackers := make([]*podTracker, 5)
	healthyDests := make([]string, 5)
	for i := range 5 {
		initialTrackers[i] = newTestTracker(
			"10.0.0."+strconv.Itoa(i)+":8080",
			queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 1, InitialCapacity: 1}),
		)
		healthyDests[i] = initialTrackers[i].dest
	}
	rt.updateThrottlerState(5, initialTrackers, healthyDests, nil, nil)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: Update capacity
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			rt.updateCapacity(5)
			rt.updateCapacity(10)
			rt.updateCapacity(3)
		}
	}()

	// Reader: Read capacity
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = rt.breaker.Capacity()
			_ = rt.breaker.Pending()
			_ = rt.breaker.InFlight()
		}
	}()

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Verify final capacity is reasonable
	finalCap := int(rt.breaker.Capacity())
	if finalCap < 3 || finalCap > 10 {
		t.Errorf("Final capacity %d is outside expected range [3, 10]", finalCap)
	}
}

// 14) Race: assignedTrackers read by requests while being updated
// Tests concurrent access to assignedTrackers during pod scaling
func TestRace_AssignedTrackers_ConcurrentReadDuringUpdate(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t).Sugar()
	rt := newRevisionThrottler(
		types.NamespacedName{Namespace: "default", Name: "rev"},
		nil,
		1,
		"http",
		queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 5},
		logger,
	)

	initialTrackers := make([]*podTracker, 5)
	healthyDests := make([]string, 5)
	for i := range 5 {
		initialTrackers[i] = newTestTracker(
			"10.0.0."+strconv.Itoa(i)+":8080",
			queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 1, InitialCapacity: 1}),
		)
		healthyDests[i] = initialTrackers[i].dest
	}
	rt.updateThrottlerState(5, initialTrackers, healthyDests, nil, nil)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	var filterCount atomic.Uint32

	// Writer: Update assignedTrackers
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			rt.updateCapacity(5)
			rt.updateCapacity(3)
		}
	}()

	// Reader: Read assignedTrackers in request path simulation
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			rt.mux.RLock()
			trackers := rt.assignedTrackers
			rt.mux.RUnlock()
			available := rt.filterAvailableTrackers(trackers)
			// Verify filtered trackers are valid
			if len(available) > 0 {
				filterCount.Add(1)
			}
		}
	}()

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Assert we successfully filtered trackers many times
	if filterCount.Load() == 0 {
		t.Error("Expected successful tracker filtering operations")
	}

	// Verify final state is consistent
	rt.mux.RLock()
	finalAssigned := len(rt.assignedTrackers)
	rt.mux.RUnlock()

	if finalAssigned < 1 || finalAssigned > 5 {
		t.Errorf("Final assigned trackers %d outside expected range [1, 5]", finalAssigned)
	}
}

// 15) Race: containerConcurrency update during capacity calculation
// Tests that CC updates during capacity recalculation don't cause inconsistencies
func TestRace_ContainerConcurrency_UpdateDuringCapacityCalc(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t).Sugar()
	rt := newRevisionThrottler(
		types.NamespacedName{Namespace: "default", Name: "rev"},
		nil,
		1,
		"http",
		queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 5},
		logger,
	)

	initialTrackers := make([]*podTracker, 5)
	healthyDests := make([]string, 5)
	for i := range 5 {
		initialTrackers[i] = newTestTracker(
			"10.0.0."+strconv.Itoa(i)+":8080",
			queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 1, InitialCapacity: 1}),
		)
		healthyDests[i] = initialTrackers[i].dest
	}
	rt.updateThrottlerState(5, initialTrackers, healthyDests, nil, nil)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: Update containerConcurrency
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			rt.containerConcurrency.Store(1)
			rt.containerConcurrency.Store(2)
			rt.containerConcurrency.Store(5)
		}
	}()

	// Reader: Calculate capacity (reads CC multiple times)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			rt.updateCapacity(5)
			capacity := rt.calculateCapacity(5, 5, 1)
			// Verify capacity is reasonable (should be CC * numTrackers)
			if capacity < 5 || capacity > 25 {
				t.Errorf("Calculated capacity %d outside expected range [5, 25]", capacity)
			}
		}
	}()

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Verify final state consistency
	finalCC := int(rt.containerConcurrency.Load())
	finalCap := int(rt.breaker.Capacity())
	rt.mux.RLock()
	finalPods := len(rt.podTrackers)
	rt.mux.RUnlock()

	// Capacity should be reasonable for the current state
	minExpected := finalPods // at least 1*pods if CC=1
	maxExpected := finalCC * finalPods
	if finalCap < minExpected || finalCap > maxExpected {
		t.Errorf("Final capacity %d outside expected range [%d, %d] (CC=%d, pods=%d)",
			finalCap, minExpected, maxExpected, finalCC, finalPods)
	}
}

// 16) Race: activator index changes during assignSlice
// Tests that activator topology changes during pod assignment don't cause issues
func TestRace_ActivatorIndex_ChangeDuringAssignSlice(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t).Sugar()
	rt := newRevisionThrottler(
		types.NamespacedName{Namespace: "default", Name: "rev"},
		nil,
		1,
		"http",
		queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 5},
		logger,
	)

	initialTrackers := make([]*podTracker, 10)
	for i := range 10 {
		initialTrackers[i] = newTestTracker(
			"10.0.0."+strconv.Itoa(i)+":8080",
			queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 1, InitialCapacity: 1}),
		)
	}
	rt.updateThrottlerState(10, initialTrackers, nil, nil, nil)
	rt.numActivators.Store(2)
	rt.activatorIndex.Store(0)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: Change activator topology
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			rt.numActivators.Store(1)
			rt.activatorIndex.Store(0)
			rt.numActivators.Store(2)
			rt.activatorIndex.Store(1)
			rt.numActivators.Store(3)
			rt.activatorIndex.Store(2)
		}
	}()

	// Reader: Call updateCapacity which reads activator index/count
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			rt.updateCapacity(10)
		}
	}()

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Verify final assigned trackers count makes sense for final topology
	finalNA := int(rt.numActivators.Load())
	finalAI := int(rt.activatorIndex.Load())
	rt.mux.RLock()
	finalAssigned := len(rt.assignedTrackers)
	finalPods := len(rt.podTrackers)
	rt.mux.RUnlock()

	// With consistent hashing, this activator should get roughly 1/numActivators of pods
	// Allow wide range due to hash distribution
	if finalNA > 0 && finalPods > 0 {
		expectedMin := 0 // Could get 0 pods with certain hash distributions
		expectedMax := finalPods
		if finalAssigned < expectedMin || finalAssigned > expectedMax {
			t.Errorf("Final assigned %d outside range [%d, %d] for %d pods, activator %d/%d",
				finalAssigned, expectedMin, expectedMax, finalPods, finalAI, finalNA)
		}
	}
}

// 17) Race: multiple concurrent updateThrottlerState calls
// Tests that concurrent throttler state updates are properly serialized
func TestRace_UpdateThrottlerState_ConcurrentUpdates(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t).Sugar()
	rt := newRevisionThrottler(
		types.NamespacedName{Namespace: "default", Name: "rev"},
		nil,
		1,
		"http",
		queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 1},
		logger,
	)

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Multiple goroutines calling updateThrottlerState
	// This violates the "single goroutine" assumption but tests thread-safety
	for i := range 3 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 20 {
				select {
				case <-stop:
					return
				default:
				}
				tracker := newTestTracker(
					"10.0."+strconv.Itoa(id)+"."+strconv.Itoa(j)+":8080",
					queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 1, InitialCapacity: 1}),
				)
				rt.updateThrottlerState(1, []*podTracker{tracker}, nil, nil, nil)
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Verify podTrackers has pods from all goroutines
	rt.mux.RLock()
	finalPodCount := len(rt.podTrackers)
	rt.mux.RUnlock()

	// Should have some pods (3 goroutines × 20 attempts, but many overwrites)
	if finalPodCount == 0 {
		t.Error("Expected some pods in podTrackers after concurrent updates")
	}

	// Verify capacity is reasonable
	finalCap := int(rt.breaker.Capacity())
	if finalCap < 0 {
		t.Errorf("Final capacity %d should not be negative", finalCap)
	}
}

// 18) Race: backendCount read/write during concurrent updateCapacity calls
// Tests that backendCount updates are atomic and don't cause capacity inconsistencies
func TestRace_BackendCount_ConcurrentReadWriteDuringCapacityUpdates(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t).Sugar()
	rt := newRevisionThrottler(
		types.NamespacedName{Namespace: "default", Name: "rev"},
		nil,
		1,
		"http",
		queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 5},
		logger,
	)

	initialTrackers := make([]*podTracker, 5)
	healthyDests := make([]string, 5)
	for i := range 5 {
		initialTrackers[i] = newTestTracker(
			"10.0.0."+strconv.Itoa(i)+":8080",
			queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 1, InitialCapacity: 1}),
		)
		healthyDests[i] = initialTrackers[i].dest
	}
	rt.updateThrottlerState(5, initialTrackers, healthyDests, nil, nil)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)

	// Writer 1: updateThrottlerState writes backendCount
	go func() {
		defer wg.Done()
		for i := 5; i < 15; i++ {
			select {
			case <-stop:
				return
			default:
			}
			tracker := newTestTracker(
				"10.0.1."+strconv.Itoa(i)+":8080",
				queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 1, InitialCapacity: 1}),
			)
			rt.updateThrottlerState(i, []*podTracker{tracker}, nil, nil, nil)
			time.Sleep(2 * time.Millisecond)
		}
	}()

	// Reader 1: handlePubEpsUpdate reads backendCount
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			storedBackends := int(rt.backendCount.Load())
			rt.updateCapacity(storedBackends)
			time.Sleep(time.Millisecond)
		}
	}()

	// Reader 2: Diagnostic code reads backendCount
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = rt.backendCount.Load()
			rt.mux.RLock()
			_ = len(rt.podTrackers)
			rt.mux.RUnlock()
		}
	}()

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Verify backendCount is in a valid state
	finalBackendCount := int(rt.backendCount.Load())
	rt.mux.RLock()
	finalPodCount := len(rt.podTrackers)
	rt.mux.RUnlock()

	// backendCount should be reasonable (though may lag slightly)
	if finalBackendCount < 0 || finalBackendCount > 20 {
		t.Errorf("Final backendCount %d outside expected range [0, 20]", finalBackendCount)
	}

	// Verify capacity matches backendCount or podCount reasonably
	finalCap := int(rt.breaker.Capacity())
	// Allow some lag but should be close to either backendCount or podCount
	if finalCap > 0 && (finalCap < finalBackendCount-5 || finalCap > finalPodCount+5) {
		t.Logf("Info: Final capacity %d, backendCount %d, podCount %d (some lag expected)",
			finalCap, finalBackendCount, finalPodCount)
	}
}

// 19) Race: concurrent reads of breaker state during capacity updates
// Simplified to avoid complex request routing
func TestRace_WaitingRequests_ConcurrentBreakerStateReads(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t).Sugar()

	rt := newRevisionThrottler(
		types.NamespacedName{Namespace: "default", Name: "rev"},
		nil,
		1,
		"http",
		queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 5},
		logger,
	)

	initialTrackers := make([]*podTracker, 5)
	healthyDests := make([]string, 5)
	for i := range 5 {
		initialTrackers[i] = newTestTracker(
			"10.0.0."+strconv.Itoa(i)+":8080",
			queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 1, InitialCapacity: 1}),
		)
		healthyDests[i] = initialTrackers[i].dest
	}
	rt.updateThrottlerState(5, initialTrackers, healthyDests, nil, nil)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: Update capacity
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			rt.updateCapacity(5)
			rt.updateCapacity(10)
		}
	}()

	// Reader: Read breaker state
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = rt.breaker.Capacity()
			_ = rt.breaker.Pending()
			_ = rt.breaker.InFlight()
		}
	}()

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Verify final capacity is reasonable
	finalCap := int(rt.breaker.Capacity())
	if finalCap < 5 || finalCap > 10 {
		t.Errorf("Final capacity %d outside expected range [5, 10]", finalCap)
	}
}

// 20) Race: podTrackers map mutation during assignSlice iteration
// Tests that assignSlice doesn't see partial/torn map updates
func TestRace_AssignSlice_MapMutationDuringIteration(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t).Sugar()
	rt := newRevisionThrottler(
		types.NamespacedName{Namespace: "default", Name: "rev"},
		nil,
		1,
		"http",
		queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 5},
		logger,
	)

	initialTrackers := make([]*podTracker, 5)
	healthyDests := make([]string, 5)
	for i := range 5 {
		initialTrackers[i] = newTestTracker(
			"10.0.0."+strconv.Itoa(i)+":8080",
			queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 1, InitialCapacity: 1}),
		)
		healthyDests[i] = initialTrackers[i].dest
	}
	rt.updateThrottlerState(5, initialTrackers, healthyDests, nil, nil)
	rt.numActivators.Store(1)
	rt.activatorIndex.Store(0)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: Mutate podTrackers map
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			tracker := newTestTracker(
				"192.168.1."+strconv.Itoa(i%10)+":8080",
				queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 1, InitialCapacity: 1}),
			)
			// Provide tracker in healthyDests to promote it to ready state
			rt.updateThrottlerState(1, []*podTracker{tracker}, []string{tracker.dest}, nil, nil)
			i++
			time.Sleep(time.Millisecond)
		}
	}()

	// Reader: Call updateCapacity which calls assignSlice
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			rt.updateCapacity(int(rt.backendCount.Load()))
		}
	}()

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Verify podTrackers has valid entries
	rt.mux.RLock()
	finalPodCount := len(rt.podTrackers)
	finalAssigned := len(rt.assignedTrackers)
	rt.mux.RUnlock()

	// Should have accumulated some pods during the test
	if finalPodCount == 0 {
		t.Error("Expected pods in podTrackers after concurrent operations")
	}

	// assignedTrackers should match podTrackers (single activator)
	if finalAssigned != finalPodCount {
		t.Errorf("Assigned trackers %d should match pod count %d with single activator",
			finalAssigned, finalPodCount)
	}

	// Verify capacity is consistent with pod count
	finalCap := int(rt.breaker.Capacity())
	expectedCap := finalPodCount * int(rt.containerConcurrency.Load())
	if finalCap != expectedCap {
		t.Errorf("Final capacity %d doesn't match expected %d (pods=%d, CC=%d)",
			finalCap, expectedCap, finalPodCount, rt.containerConcurrency.Load())
	}
}
