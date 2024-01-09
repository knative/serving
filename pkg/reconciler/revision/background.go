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
	Resolve(ctx context.Context, image string, opt k8schain.Options, registriesToSkip sets.Set[string]) (string, error)
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
	// these fields are immutable after creation, so can be accessed without a lock.
	opt                k8schain.Options
	registriesToSkip   sets.Set[string]
	completionCallback func()
	workItems          []workItem

	// these fields can be written concurrently, so should only be accessed while
	// holding the backgroundResolver mutex.

	// imagesResolved is a map of container image to resolved image with digest.
	imagesResolved map[string]string

	// imagesToBeResolved keeps unique image names so we can quickly compare with the current number of resolved ones
	imagesToBeResolved sets.Set[string]

	err error
}

// workItem is a single task submitted to the queue, to resolve a single image
// for a resolveResult.
type workItem struct {
	revision types.NamespacedName
	timeout  time.Duration

	image string
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
func (r *backgroundResolver) Resolve(logger *zap.SugaredLogger, rev *v1.Revision, opt k8schain.Options, registriesToSkip sets.Set[string], timeout time.Duration) (initContainerStatuses []v1.ContainerStatus, statuses []v1.ContainerStatus, error error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := types.NamespacedName{
		Name:      rev.Name,
		Namespace: rev.Namespace,
	}

	result, inFlight := r.results[name]
	if !inFlight {
		logger.Debugf("Adding Resolve request to queue (depth: %d)", r.queue.Len())
		r.addWorkItems(rev, name, opt, registriesToSkip, timeout)
		return nil, nil, nil
	}

	if !result.ready() {
		logger.Debug("Resolve request in flight, returning nil, nil")
		return nil, nil, nil
	}

	ret := r.results[name]
	if ret.err != nil {
		logger.Debugf("Resolve returned the resolved error: %v", ret.err)
		return nil, nil, ret.err
	}

	initContainerStatuses = make([]v1.ContainerStatus, len(rev.Spec.InitContainers))
	for i, container := range rev.Spec.InitContainers {
		resolvedDigest := ret.imagesResolved[container.Image]
		initContainerStatuses[i] = v1.ContainerStatus{
			Name:        container.Name,
			ImageDigest: resolvedDigest,
		}
	}

	statuses = make([]v1.ContainerStatus, len(rev.Spec.Containers))
	for i, container := range rev.Spec.Containers {
		resolvedDigest := ret.imagesResolved[container.Image]
		statuses[i] = v1.ContainerStatus{
			Name:        container.Name,
			ImageDigest: resolvedDigest,
		}
	}

	logger.Debugf("Resolve returned %d resolved images for revision", len(statuses)+len(initContainerStatuses))
	return initContainerStatuses, statuses, nil
}

// addWorkItems adds a digest resolve item to the queue for each container in the revision.
// This is expected to be called with the mutex locked.
func (r *backgroundResolver) addWorkItems(rev *v1.Revision, name types.NamespacedName, opt k8schain.Options, registriesToSkip sets.Set[string], timeout time.Duration) {
	totalNumOfContainers := len(rev.Spec.Containers) + len(rev.Spec.InitContainers)
	r.results[name] = &resolveResult{
		opt:                opt,
		registriesToSkip:   registriesToSkip,
		imagesResolved:     make(map[string]string),
		imagesToBeResolved: sets.Set[string]{},
		workItems:          make([]workItem, 0, totalNumOfContainers),
		completionCallback: func() {
			r.enqueue(name)
		},
	}

	for _, container := range append(rev.Spec.InitContainers, rev.Spec.Containers...) {
		if r.results[name].imagesToBeResolved.Has(container.Image) {
			continue
		}
		item := workItem{
			revision: name,
			timeout:  timeout,
			image:    container.Image,
		}
		r.results[name].workItems = append(r.results[name].workItems, item)
		r.results[name].imagesToBeResolved.Insert(container.Image)
		r.queue.AddRateLimited(item)
	}
}

// processWorkItem runs a single image digest resolution and stores the result
// in the resolveResult. If this completes the work for the revision, the
// completionCallback is called.
func (r *backgroundResolver) processWorkItem(item workItem) {
	defer r.queue.Done(item)
	r.logger.Debugf("Processing image %q from revision %q (depth: %d)", item.image, item.revision, r.queue.Len())

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

	r.logger.Debugf("Resolving image %q from revision %q to digest", item.image, item.revision)
	resolvedDigest, resolveErr := r.resolver.Resolve(ctx, item.image, result.opt, result.registriesToSkip)
	r.logger.Debugf("Resolved image %q from revision %q to digest %q, %v", item.image, item.revision, resolvedDigest, resolveErr)

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
		result.err = fmt.Errorf("%s: %w", v1.RevisionContainerMissingMessage(item.image, "failed to resolve image to digest"), resolveErr)
		result.completionCallback()
		return
	}

	result.imagesResolved[item.image] = resolvedDigest

	if result.ready() {
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
	return len(r.imagesToBeResolved) == len(r.imagesResolved) || r.err != nil
}
