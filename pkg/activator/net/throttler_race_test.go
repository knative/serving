package net

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/serving/pkg/queue"
)

func newRaceTestRT(t *testing.T) *revisionThrottler {
	t.Helper()
	logger := zaptest.NewLogger(t, zaptest.WrapOptions(zap.AddCaller())).Sugar()
	rt := newRevisionThrottler(
		types.NamespacedName{Namespace: "default", Name: "rev"},
		nil,
		1,
		"http",
		queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 1},
		logger,
	)
	return rt
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
			tr := newPodTracker(addr+":8080", queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
			rt.updateThrottlerState(1, []*podTracker{tr}, []string{tr.dest}, nil, nil)
			rt.updateThrottlerState(0, nil, nil, []string{tr.dest}, nil)
			i++
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx := context.Background()
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
			_ = rt.filterAvailableTrackers(ctx, local)
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
		tr := newPodTracker("10.0.0."+string(rune('a'+i))+":8080", queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
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
				rt.updateThrottlerState(1, []*podTracker{newPodTracker(addr, nil)}, nil, nil, nil)
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

// 3) Use-after-delete style: remove tracker while try() is running
func TestRace_RequestPath_TrackerRemovedDuringUse(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t).Sugar()
	rt := newRevisionThrottler(types.NamespacedName{Namespace: "ns", Name: "rev"}, nil, 1, "http", queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10}, logger)

	tr := newPodTracker("127.0.0.1:65534", queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
	rt.updateThrottlerState(1, []*podTracker{tr}, []string{tr.dest}, nil, nil)

	oldPing := tcpPingCheckFunc.Load()
	setTCPPingCheckFunc(func(_ string) bool { return true })
	defer func() { tcpPingCheckFunc.Store(oldPing) }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			_ = rt.try(ctx, "x", func(_ string, _ bool) error { time.Sleep(time.Microsecond); return errors.New("fail fast") })
		}
	}()
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			rt.updateThrottlerState(0, nil, nil, []string{tr.dest}, nil)
			rt.updateThrottlerState(1, []*podTracker{tr}, []string{tr.dest}, nil, nil)
		}
	}()
	wg.Wait()
}

// 4) Global variable mutation race
func TestRace_TCPPingCheckFunc_GlobalMutation(t *testing.T) {
	t.Parallel()
	orig := tcpPingCheckFunc.Load()
	defer func() { tcpPingCheckFunc.Store(orig) }()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			setTCPPingCheckFunc(func(string) bool { return true })
			setTCPPingCheckFunc(func(string) bool { return false })
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = tcpPingCheck("localhost:1")
		}
	}()
	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// 5) LoadBalancer policy race: policy update vs request path usage
func TestRace_LBPolicy_UpdateVsUsage(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t).Sugar()
	rt := newRevisionThrottler(types.NamespacedName{Namespace: "default", Name: "rev"}, nil, 1, "http", queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 1}, logger)

	// Add some initial trackers
	tr1 := newPodTracker("10.0.0.1:8080", queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
	tr2 := newPodTracker("10.0.0.2:8080", queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
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
		tr := newPodTracker("10.0.0."+strconv.Itoa(i)+":8080", queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
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
				rt.updateThrottlerState(1, []*podTracker{newPodTracker(addr, queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))}, nil, nil, nil)
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

// 10) Pod state transitions race: multiple goroutines changing state
func TestRace_PodStateTransitions_ConcurrentStateChanges(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t).Sugar()
	rt := newRevisionThrottler(types.NamespacedName{Namespace: "default", Name: "rev"}, nil, 1, "http", queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 1}, logger)

	tr := newPodTracker("127.0.0.1:8080", queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
	rt.updateThrottlerState(1, []*podTracker{tr}, []string{tr.dest}, nil, nil)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)

	// Goroutine 1: quarantine pod
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			tr.state.Store(uint32(podQuarantined))
			time.Sleep(time.Microsecond)
		}
	}()

	// Goroutine 2: mark pod healthy
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			tr.state.Store(uint32(podHealthy))
			time.Sleep(time.Microsecond)
		}
	}()

	// Goroutine 3: try to drain pod
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			tr.tryDrain() // CAS from healthy to draining
			time.Sleep(time.Microsecond)
		}
	}()

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}
