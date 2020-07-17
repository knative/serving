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

package controller

import (
	"fmt"

	"k8s.io/client-go/util/workqueue"
)

// item wraps workqueue.Interface.Get() return
type item struct {
	i  interface{}
	sd bool
}

// twoLaneQueue is a rate limited queue that wraps around two queues
// -- fast queue, whose contents are processed with priority.
// -- slow queue, whose contents are processed if fast queue has no items.
// All the default methods operate on the fast queue, unless noted otherwise.
type twoLaneQueue struct {
	workqueue.RateLimitingInterface
	slowLane workqueue.RateLimitingInterface
	// consumerQueue is necessary to ensure that we're not reconciling
	// the same object at the exact same time (e.g. if it had been enqueued
	// in both fast and slow and is the only object there).
	consumerQueue workqueue.Interface

	name string

	fastChan chan item
	slowChan chan item
}

func NewTwoLaneQueue(name string) workqueue.RateLimitingInterface {
	rl := workqueue.DefaultControllerRateLimiter()
	tlq := &twoLaneQueue{
		RateLimitingInterface: workqueue.NewNamedRateLimitingQueue(
			rl,
			name+"-fast",
		),
		slowLane: workqueue.NewNamedRateLimitingQueue(
			rl,
			name+"-slow",
		),
		consumerQueue: workqueue.NewNamed(name + "-consumer"),
		name:          name,
		fastChan:      make(chan item),
		slowChan:      make(chan item),
	}
	tlq.runConsumer()
	tlq.runProducers()
	return tlq
}

func process(q workqueue.Interface, ch chan item) {
	go func(q workqueue.Interface) {
		for {
			i, d := q.Get()
			// If the queue is empty and we're shutting down — stop the loop.
			if d {
				break
			}
			fmt.Println("### read", i, d)
			ch <- item{i: i, sd: d}
		}
		// Sender closes the channel
		close(ch)
	}(q)
}

func (tlq *twoLaneQueue) runProducers() {
	process(tlq.RateLimitingInterface, tlq.fastChan)
	process(tlq.slowLane, tlq.slowChan)
}

func (tlq *twoLaneQueue) runConsumer() {
	tlq.consumerQueue = workqueue.NewNamed(tlq.name + "consumer")
	// Shutdown flags.
	fast, slow := true, true
	go func() {
		for fast || slow {
			// By default drain the fast lane.
			if fast {
				select {
				case item, ok := <-tlq.fastChan:
					if !ok {
						fmt.Println(tlq.name, "TLQ: fast queue done")
						fast = false
						continue
					}
					fmt.Println(tlq.name, "TLQ: reading from the fast lane", item.i, item.sd)
					tlq.consumerQueue.Add(item.i)
					continue
				default:
				}
			}
			// Fast lane is empty, so wait for either channel to trigger. Since
			// channels are picked randomly any of the lanes might trigger.
			select {
			case item, ok := <-tlq.fastChan:
				if !ok {
					fmt.Println(tlq.name, "TLQ: fast queue done")
					fast = false
					continue
				}
				fmt.Println(tlq.name, "TLQ: reading from the fast lane, 2", item.i, item.sd)
				tlq.consumerQueue.Add(item.i)
			case item, ok := <-tlq.slowChan:
				if !ok {
					fmt.Println(tlq.name, "TLQ: slow queue done")
					slow = false
					continue
				}
				fmt.Println(tlq.name, "TLQ: reading from the slow lane", item.i, item.sd)
				tlq.consumerQueue.Add(item.i)
			}
		}
		fmt.Println(tlq.name, "TLQ: both producers shutdown; killing consumer")
		// We know both queues are shutdown now. Stop the consumerQueue.
		tlq.consumerQueue.ShutDown()
	}()
}

// Shutdown implements workqueue.Interace.
// Shutdown shuts down both queues.
func (tlq *twoLaneQueue) ShutDown() {
	tlq.RateLimitingInterface.ShutDown()
	tlq.slowLane.ShutDown()
}

// Done implements workqueue.Interface.
// Done marks the item as completed in both queues.
// NB: this will just re-enqueue the object on the queue that
// didn't originate the object.
func (tlq *twoLaneQueue) Done(i interface{}) {
	// Mark the item as done in the consumer queue, so it can be removed.
	tlq.consumerQueue.Done(i)
	tlq.RateLimitingInterface.Done(i)
	tlq.slowLane.Done(i)
}

// Get implements workqueue.Interface.
// It gets the item from fast lane if it has anything, alternatively
// the slow lane.
func (tlq *twoLaneQueue) Get() (interface{}, bool) {
	return tlq.consumerQueue.Get()
}

// Len returns the sum of lengths.
// NB: actual _number_ of unique object might be less than this sum.
func (tlq *twoLaneQueue) Len() int {
	return tlq.RateLimitingInterface.Len() + tlq.slowLane.Len() + tlq.consumerQueue.Len()
}

// SlowLane gives direct access to the slow queue.
func (tlq *twoLaneQueue) SlowLane() workqueue.RateLimitingInterface {
	return tlq.slowLane
}
