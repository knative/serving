/*
Licensed under the Apache License, Version 2.0
*/

package queue

import (
	"context"
	"fmt"

	"go.uber.org/atomic"
	"knative.dev/serving/pkg/queue/spmetricserver"
)

// ResourceBreakerParams defines the parameters of the ResourceBreaker.
type ResourceBreakerParams struct {
	QueueDepthUnits              int           // Queue depth in resource units (e.g., memory in MB)
	MaxConcurrencyUnits          int           // Max concurrency in resource units
	InitialCapacityUnits         int           // Initial capacity in resource units
	ResourceUtilizationThreshold float64       // Resource utilization threshold
	MetricValue                  *atomic.Value // Superpod metric value
}

// ResourceBreaker enforces a concurrency limit on the execution of functions,
// and maintains a queue of function executions exceeding the concurrency limit.
type ResourceBreaker struct {
	inFlight   atomic.Int64 // Current in-flight resource units (e.g., memory in MB)
	totalSlots int64        // Total slots available in the queue (resource units)
	sem        *resourceSemaphore

	// release is the callback function returned to callers by Reserve to
	// allow the reservation made by Reserve to be released.
	release func(resourceUnits uint64)
}

// NewResourceBreaker creates a ResourceBreaker with the desired queue depth,
// concurrency limit, and initial capacity.
func NewResourceBreaker(params ResourceBreakerParams) *ResourceBreaker {
	if params.QueueDepthUnits <= 0 {
		panic(fmt.Sprintf("Queue depth must be greater than 0. Got %v.", params.QueueDepthUnits))
	}
	if params.QueueDepthUnits < params.MaxConcurrencyUnits || params.QueueDepthUnits < params.InitialCapacityUnits {
		panic(fmt.Sprintf("Queue depth must be greater or equal to the concurrenct resource request units. Got %v.", params.QueueDepthUnits))
	}
	if params.MaxConcurrencyUnits < 0 {
		panic(fmt.Sprintf("Max concurrency must be 0 or greater. Got %v.", params.MaxConcurrencyUnits))
	}
	if params.InitialCapacityUnits < 0 || params.InitialCapacityUnits > params.MaxConcurrencyUnits {
		panic(fmt.Sprintf("Initial capacity must be between 0 and max concurrency. Got %v.", params.InitialCapacityUnits))
	}

	b := &ResourceBreaker{
		totalSlots: int64(params.QueueDepthUnits),
		sem:        newResourceSemaphore(uint64(params.InitialCapacityUnits), params.MetricValue, params.ResourceUtilizationThreshold),
	}

	// Allocating the closure returned by Reserve here avoids an allocation in Reserve.
	b.release = func(resourceUnits uint64) {
		b.sem.release(resourceUnits)
		b.releasePending(resourceUnits)
	}

	return b
}

// tryAcquirePending tries to acquire resource units on the pending "queue".
func (b *ResourceBreaker) tryAcquirePending(resourceUnits uint64) bool {
	for {
		cur := b.inFlight.Load()
		if cur+int64(resourceUnits) > b.totalSlots {
			return false
		}
		if b.inFlight.CAS(cur, cur+int64(resourceUnits)) {
			return true
		}
	}
}

// releasePending releases resource units on the pending "queue".
func (b *ResourceBreaker) releasePending(resourceUnits uint64) {
	b.inFlight.Add(-int64(resourceUnits))
}

// Reserve reserves execution slots in the breaker based on resource units.
// The caller on success must execute the callback when done with work.
func (b *ResourceBreaker) Reserve(ctx context.Context, resourceUnits uint64) (func(), bool) {
	if !b.tryAcquirePending(resourceUnits) {
		return nil, false
	}

	if !b.sem.tryAcquire(resourceUnits) {
		b.releasePending(resourceUnits)
		return nil, false
	}

	return func() {
		b.release(resourceUnits)
	}, true
}

// Maybe conditionally executes thunk based on the ResourceBreaker concurrency
// and queue parameters. If the concurrency limit and queue capacity are
// already consumed, Maybe returns immediately without calling thunk.
func (b *ResourceBreaker) Maybe(ctx context.Context, resourceUnits uint64, thunk func()) error {
	if !b.tryAcquirePending(resourceUnits) {
		return ErrRequestQueueFull
	}

	defer b.releasePending(resourceUnits)

	// Wait for capacity in the active queue.
	if err := b.sem.acquire(ctx, resourceUnits); err != nil {
		return err
	}
	// Defer releasing capacity in the active queue.
	defer b.sem.release(resourceUnits)

	// Execute the function.
	thunk()
	// Report success.
	return nil
}

// InFlight returns the number of resource units currently in flight in this breaker.
func (b *ResourceBreaker) InFlight() int {
	return int(b.inFlight.Load())
}

// UpdateConcurrency updates the maximum number of in-flight resource units.
func (b *ResourceBreaker) UpdateConcurrency(size int) {
	b.sem.updateCapacity(uint64(size))
}

// Capacity returns the number of allowed in-flight resource units on this breaker.
func (b *ResourceBreaker) Capacity() uint64 {
	return b.sem.Capacity()
}

// newResourceSemaphore creates a resourceSemaphore with the desired initial capacity.
func newResourceSemaphore(initialCapacity uint64, metricValue *atomic.Value, resourceUtilizationThreshold float64) *resourceSemaphore {
	queue := make(chan struct{}, 1) // Use a buffered channel with capacity 1 for notification
	sem := &resourceSemaphore{queue: queue, metricValue: metricValue, resourceUtilizationThreshold: resourceUtilizationThreshold}
	sem.updateCapacity(initialCapacity)
	return sem
}

// resourceSemaphore is an implementation of a semaphore based on packed integers and a channel.
// The state is a uint64 that packs two uint64s: capacity and inFlight.
// The channel is used to notify waiting goroutines when capacity becomes available.
type resourceSemaphore struct {
	state                        atomic.Uint64
	queue                        chan struct{}
	metricValue                  *atomic.Value
	resourceUtilizationThreshold float64
}

// tryAcquire acquires resource units from the semaphore if there is enough capacity.
func (s *resourceSemaphore) tryAcquire(resourceUnits uint64) bool {
	for {
		old := s.state.Load()
		capacity, in := unpack(old)
		if in+resourceUnits > capacity {
			return false
		}
		newIn := in + resourceUnits
		if s.state.CAS(old, pack(capacity, newIn)) {
			return true
		}
	}
}

// acquire waits until enough capacity is available to acquire the specified resource units.
func (s *resourceSemaphore) acquire(ctx context.Context, resourceUnits uint64) error {
	for {
		old := s.state.Load()
		capacity, in := unpack(old)

		if in+resourceUnits > capacity || s.metricValue.Load().(spmetricserver.SuperPodMetrics).CpuUtilization > s.resourceUtilizationThreshold {
			// if (total memory limit > 12GB, which can be slightly higher than the actual sp size)  || (current CPU utilization > threshold) {hold the request}
			// resource utilization threshold can be a large value (but smaller than the capacity of the node) since burstable pods without CPU limit are used
			// a superpod can use idle CPU on the node freely up to the node's total CPU capacity
			// e.g., if (total memory limit > 12GB)  || (total cpu limit > 6) {hold the request}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-s.queue:
			}
			// Force reload state.
			continue
		}

		newIn := in + resourceUnits
		if s.state.CAS(old, pack(capacity, newIn)) {
			return nil
		}
	}
}

// release releases resource units back to the semaphore.
func (s *resourceSemaphore) release(resourceUnits uint64) {
	for {
		old := s.state.Load()
		capacity, in := unpack(old)

		if in < resourceUnits {
			panic("release and acquire are not properly paired")
		}

		newIn := in - resourceUnits
		if s.state.CAS(old, pack(capacity, newIn)) {
			// Notify waiting goroutines that capacity may be available.
			select {
			case s.queue <- struct{}{}:
			default:
				// Notification channel is full or no goroutines are waiting.
			}
			return
		}
	}
}

// updateCapacity updates the capacity of the semaphore to the desired size.
func (s *resourceSemaphore) updateCapacity(newCapacity uint64) {
	for {
		old := s.state.Load()
		_, in := unpack(old)

		if s.state.CAS(old, pack(newCapacity, in)) {
			// Notify waiting goroutines if capacity increased.
			select {
			case s.queue <- struct{}{}:
			default:
			}
			return
		}
	}
}

// Capacity returns the capacity of the semaphore.
func (s *resourceSemaphore) Capacity() uint64 {
	capacity, _ := unpack(s.state.Load())
	return capacity
}
