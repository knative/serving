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

package revision

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn/k8schain"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

// imageResolver is an interface used mostly to mock digestResolver for tests.
type imageResolver interface {
	Resolve(ctx context.Context, image string, opt k8schain.Options, registriesToSkip sets.String) (string, error)
}

// backgroundResolver performs background downloads of image digests.
type backgroundResolver struct {
	logger *zap.SugaredLogger

	resolver imageResolver
	enqueue  func(types.NamespacedName)

	queue workqueue.RateLimitingInterface

	mu      sync.RWMutex
	results map[types.NamespacedName]*resolveResult
}

// resolveResult is the overall result for a particular revision. We create a
// workItem for each container we need to resolve for the overall result.
type resolveResult struct {
	// these fields are immutable afer creation, so can be accessed without a lock.
	opt                k8schain.Options
	registriesToSkip   sets.String
	completionCallback func()
	workItems          []workItem

	// these fields can be written concurrently, so should only be accessed while
	// holding the backgroundResolver mutex.

	// statuses is a map of container index to resolved status. This is a map
	// rather than an array so that we can easily determine when we have received
	// all the digests that we want (by comparing length to `want`).
	statuses map[int]v1.ContainerStatus
	want     int
	err      error
}

// workItem is a single task submitted to the queue, to resolve a single image
// for a resolveResult.
type workItem struct {
	revision types.NamespacedName
	timeout  time.Duration

	name  string
	image string
	index int
}

func newBackgroundResolver(logger *zap.SugaredLogger, resolver imageResolver, queue workqueue.RateLimitingInterface, enqueue func(types.NamespacedName)) *backgroundResolver {
	r := &backgroundResolver{
		logger: logger,

		resolver: resolver,
		enqueue:  enqueue,

		results: make(map[types.NamespacedName]*resolveResult),
		queue:   queue,
	}

	return r
}

// Start starts the worker threads and runs maxInFlight workers until the stop
// channel is closed. It returns a done channel which will be closed when all
// workers have exited.
func (r *backgroundResolver) Start(stop <-chan struct{}, maxInFlight int) (done chan struct{}) {
	var wg sync.WaitGroup

	// Run the worker threads.
	wg.Add(maxInFlight)
	for i := 0; i < maxInFlight; i++ {
		go func() {
			defer wg.Done()
			for {
				start := time.Now()
				defer func() {
					end := time.Now()
					r.logger.Info("process item duration", end.Sub(start).String())
				}()

				item, shutdown := r.queue.Get()
				if shutdown {
					return
				}

				rrItem, ok := item.(workItem)
				if !ok {
					r.logger.Fatalf("Unexpected work item type: want: %T, got: %T", workItem{}, item)
				}

				r.processWorkItem(rrItem)
			}
		}()
	}

	// Shut down the queue once the stop channel is closed, this will cause all
	// the workers to exit.
	go func() {
		<-stop
		r.queue.ShutDown()
	}()

	// Return a done channel which is closed once all workers exit.
	done = make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	return done
}

// Resolve is intended to be called from a reconciler to resolve a revision. If
// the resolver already has the digest in cache it is returned immediately, if
// it does not and no resolution is already in flight a resolution is triggered
// in the background.
// If this method returns `nil, nil` this implies a resolve was triggered or is
// already in progress, so the reconciler should exit and wait for the revision
// to be re-enqueued when the result is ready.
func (r *backgroundResolver) Resolve(rev *v1.Revision, opt k8schain.Options, registriesToSkip sets.String, timeout time.Duration) ([]v1.ContainerStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := types.NamespacedName{
		Name:      rev.Name,
		Namespace: rev.Namespace,
	}

	result, inFlight := r.results[name]
	if !inFlight {
		r.addWorkItems(rev, name, opt, registriesToSkip, timeout)
		return nil, nil
	}

	if !result.ready() {
		return nil, nil
	}

	ret := r.results[name]
	if ret.err != nil {
		return nil, ret.err
	}

	array := make([]v1.ContainerStatus, len(rev.Spec.Containers))
	for k, v := range ret.statuses {
		array[k] = v
	}

	return array, ret.err
}

// addWorkItems adds a digest resolve item to the queue for each container in the revision.
// This is expected to be called with the mutex locked.
func (r *backgroundResolver) addWorkItems(rev *v1.Revision, name types.NamespacedName, opt k8schain.Options, registriesToSkip sets.String, timeout time.Duration) {
	r.results[name] = &resolveResult{
		opt:              opt,
		registriesToSkip: registriesToSkip,
		statuses:         make(map[int]v1.ContainerStatus),
		want:             len(rev.Spec.Containers),
		workItems:        make([]workItem, len(rev.Spec.Containers)),
		completionCallback: func() {
			r.enqueue(name)
		},
	}

	for i, container := range rev.Spec.Containers {
		item := workItem{
			revision: name,
			timeout:  timeout,
			name:     container.Name,
			image:    container.Image,
			index:    i,
		}

		r.results[name].workItems[i] = item
		r.queue.AddRateLimited(item)
	}
}

// processWorkItem runs a single image digest resolution and stores the result
// in the resolveResult. If this completes the work for the revision, the
// completionCallback is called.
func (r *backgroundResolver) processWorkItem(item workItem) {
	defer r.queue.Done(item)

	// We need to acquire the result under lock since it's theoretically possible
	// for a Clear to race with this and try to delete the result from the map.
	r.mu.RLock()
	result := r.results[item.revision]
	r.mu.RUnlock()

	if result == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), item.timeout)
	defer cancel()
	start := time.Now()
	resolvedDigest, resolveErr := r.resolver.Resolve(ctx, item.image, result.opt, result.registriesToSkip)
	end := time.Now()

	r.logger.Info("duration to resolve", item.image, end.Sub(start).String())

	// lock after the resolve because we don't want to block parallel resolves,
	// just storing the result.
	r.mu.Lock()
	defer r.mu.Unlock()

	// If we succeeded we can stop remembering the item for back-off purposes.
	if resolveErr == nil {
		r.queue.Forget(item)
	}

	// If we're already ready we don't want to callback twice.
	// This can happen if an image resolve completes but we've already reported
	// an error from another image in the result.
	if result.ready() {
		return
	}

	if resolveErr != nil {
		result.statuses = nil
		result.err = fmt.Errorf("%s: %w", v1.RevisionContainerMissingMessage(item.image, "failed to resolve image to digest"), resolveErr)
		result.completionCallback()
		r.logger.Info("signal error", item.image, result.err)
		return
	}

	result.statuses[item.index] = v1.ContainerStatus{
		Name:        item.name,
		ImageDigest: resolvedDigest,
	}

	if result.ready() {
		r.logger.Info("signal completion", item.image)
		result.completionCallback()
	}
}

// Clear removes any cached results for the revision. This should be called
// once the revision's ContainerStatus has been set.
func (r *backgroundResolver) Clear(name types.NamespacedName) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.results, name)
}

// Forget removes the revision from the rate limiter and removes any cached
// results for the revision. It should be called when the revision is deleted
// or marked permanently failed.
func (r *backgroundResolver) Forget(name types.NamespacedName) {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := r.results[name]
	if result == nil {
		return
	}

	for _, item := range result.workItems {
		r.queue.Forget(item)
	}

	delete(r.results, name)
}

func (r *resolveResult) ready() bool {
	return len(r.statuses) == r.want || r.err != nil
}
