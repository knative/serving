/*
Copyright 2019 The Knative Authors.

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

package status

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"

	"knative.dev/pkg/network/prober"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/network/ingress"
)

const (
	// probeConcurrency defines how many probing calls can be issued simultaneously
	probeConcurrency = 15
	// stateExpiration defines how long after being last accessed a state expires
	stateExpiration = 5 * time.Minute
	// cleanupPeriod defines how often states are cleaned up
	cleanupPeriod = 1 * time.Minute
)

var dialContext = (&net.Dialer{}).DialContext

// ingressState represents the probing progress at the Ingress scope
type ingressState struct {
	hash string
	ia   *v1alpha1.Ingress

	// pendingCount is the number of pods that haven't been successfully probed yet
	pendingCount int32
	lastAccessed time.Time

	cancel func()
}

// podState represents the probing progress at the Pod scope
type podState struct {
	successCount int32

	context context.Context
	cancel  func()
}

type workItem struct {
	ingressState *ingressState
	podState     *podState
	url          string
	podIP        string
	podPort      string
}

// ProbeTargetLister lists all the targets that requires probing.
type ProbeTargetLister interface {

	// ListProbeTargets returns the target to be probed as a map from podIP -> port -> urls.
	ListProbeTargets(ctx context.Context, ingress *v1alpha1.Ingress) (map[string]map[string]sets.String, error)
}

// Manager provides a way to check if an Ingress is ready
type Manager interface {
	IsReady(ctx context.Context, ia *v1alpha1.Ingress) (bool, error)
}

// Prober provides a way to check if a VirtualService is ready by probing the Envoy pods
// handling that VirtualService.
type Prober struct {
	logger *zap.SugaredLogger

	// mu guards ingressStates and podStates
	mu            sync.Mutex
	ingressStates map[string]*ingressState
	podStates     map[string]*podState

	workQueue workqueue.RateLimitingInterface

	targetLister ProbeTargetLister

	readyCallback func(*v1alpha1.Ingress)

	probeConcurrency int
	stateExpiration  time.Duration
	cleanupPeriod    time.Duration
}

// NewProber creates a new instance of Prober
func NewProber(
	logger *zap.SugaredLogger,
	targetLister ProbeTargetLister,
	readyCallback func(*v1alpha1.Ingress)) *Prober {
	return &Prober{
		logger:        logger,
		ingressStates: make(map[string]*ingressState),
		podStates:     make(map[string]*podState),
		workQueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"ProbingQueue"),
		targetLister:     targetLister,
		readyCallback:    readyCallback,
		probeConcurrency: probeConcurrency,
		stateExpiration:  stateExpiration,
		cleanupPeriod:    cleanupPeriod,
	}
}

func ingressKey(ing *v1alpha1.Ingress) string {
	return fmt.Sprintf("%s/%s", ing.GetNamespace(), ing.GetName())
}

// IsReady checks if the provided Ingress is ready, i.e. the Envoy pods serving the Ingress
// have all been updated. This function is designed to be used by the Ingress controller, i.e. it
// will be called in the order of reconciliation. This means that if IsReady is called on an Ingress,
// this Ingress is the latest known version and therefore anything related to older versions can be ignored.
// Also, it means that IsReady is not called concurrently.
func (m *Prober) IsReady(ctx context.Context, ia *v1alpha1.Ingress) (bool, error) {
	ingressKey := ingressKey(ia)

	bytes, err := ingress.ComputeHash(ia)
	if err != nil {
		return false, fmt.Errorf("failed to compute the hash of the Ingress: %w", err)
	}
	hash := fmt.Sprintf("%x", bytes)

	if ready, ok := func() (bool, bool) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if state, ok := m.ingressStates[ingressKey]; ok {
			if state.hash == hash {
				state.lastAccessed = time.Now()
				return atomic.LoadInt32(&state.pendingCount) == 0, true
			}

			// Cancel the polling for the outdated version
			state.cancel()
			delete(m.ingressStates, ingressKey)
		}
		return false, false
	}(); ok {
		return ready, nil
	}

	ingCtx, cancel := context.WithCancel(context.Background())
	ingressState := &ingressState{
		hash:         hash,
		ia:           ia,
		lastAccessed: time.Now(),
		cancel:       cancel,
	}

	var workItems []*workItem

	allProbeTargets, err := m.targetLister.ListProbeTargets(ctx, ia)
	if err != nil {
		return false, err
	}

	for ip, targets := range allProbeTargets {
		// Each Pod is probed using the different hosts, protocol and ports until
		// one of the probing calls succeeds. Then, the Pod is considered ready and all pending work items
		// scheduled for that pod are cancelled.
		ctx, cancel := context.WithCancel(ingCtx)
		podState := &podState{
			successCount: 0,
			context:      ctx,
			cancel:       cancel,
		}

		// Save the podState to be able to cancel it in case of Pod deletion
		func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.podStates[ip] = podState
		}()

		// Update states and cleanup m.podStates when probing is done or cancelled
		go func(ip string) {
			<-podState.context.Done()
			m.updateStates(ingressState, podState)

			m.mu.Lock()
			defer m.mu.Unlock()
			// It is critical to check that the current podState is also the one stored in the map
			// before deleting it because it could have been replaced if a new version of the ingress
			// has started being probed.
			if state, ok := m.podStates[ip]; ok && state == podState {
				delete(m.podStates, ip)
			}
		}(ip)

		for port, urls := range targets {
			for _, url := range urls.List() {
				workItem := &workItem{
					ingressState: ingressState,
					podState:     podState,
					url:          url,
					podIP:        ip,
					podPort:      port,
				}
				workItems = append(workItems, workItem)
			}
		}
		ingressState.pendingCount++
	}
	func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.ingressStates[ingressKey] = ingressState
	}()
	for _, workItem := range workItems {
		m.workQueue.AddRateLimited(workItem)
		m.logger.Infof("Queuing probe for %s, IP: %s:%s (depth: %d)", workItem.url, workItem.podIP, workItem.podPort, m.workQueue.Len())
	}
	return len(workItems) == 0, nil
}

// Start starts the Manager background operations
func (m *Prober) Start(done <-chan struct{}) {
	// Start the worker goroutines
	for i := 0; i < m.probeConcurrency; i++ {
		go func() {
			for m.processWorkItem() {
			}
		}()
	}

	// Cleanup the states periodically
	go wait.Until(m.expireOldStates, m.cleanupPeriod, done)

	// Stop processing the queue when cancelled
	go func() {
		<-done
		m.workQueue.ShutDown()
	}()
}

// CancelIngressProbing cancels probing of the provided Ingress
func (m *Prober) CancelIngressProbing(ing *v1alpha1.Ingress) {
	key := ingressKey(ing)

	m.mu.Lock()
	defer m.mu.Unlock()
	if state, ok := m.ingressStates[key]; ok {
		state.cancel()
		delete(m.ingressStates, key)
	}
}

// CancelPodProbing cancels probing of the provided Pod IP.
func (m *Prober) CancelPodProbing(pod *corev1.Pod) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if state, ok := m.podStates[pod.Status.PodIP]; ok {
		state.cancel()
	}
}

// expireOldStates removes the states that haven't been accessed in a while.
func (m *Prober) expireOldStates() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, state := range m.ingressStates {
		if time.Since(state.lastAccessed) > m.stateExpiration {
			state.cancel()
			delete(m.ingressStates, key)
		}
	}
}

// processWorkItem processes a single work item from workQueue.
// It returns false when there is no more items to process, true otherwise.
func (m *Prober) processWorkItem() bool {
	obj, shutdown := m.workQueue.Get()
	if shutdown {
		return false
	}

	defer m.workQueue.Done(obj)

	// Crash if the item is not of the expected type
	item, ok := obj.(*workItem)
	if !ok {
		m.logger.Fatalf("Unexpected work item type: want: %s, got: %s\n", reflect.TypeOf(&workItem{}).Name(), reflect.TypeOf(obj).Name())
	}
	m.logger.Infof("Processing probe for %s, IP: %s:%s (depth: %d)", item.url, item.podIP, item.podPort, m.workQueue.Len())

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			// We only want to know that the Gateway is configured, not that the configuration is valid.
			// Therefore, we can safely ignore any TLS certificate validation.
			InsecureSkipVerify: true,
		},
		DialContext: func(ctx context.Context, network, addr string) (conn net.Conn, e error) {
			// Requests with the IP as hostname and the Host header set do no pass client-side validation
			// because the HTTP client validates that the hostname (not the Host header) matches the server
			// TLS certificate Common Name or Alternative Names. Therefore, http.Request.URL is set to the
			// hostname and it is substituted it here with the target IP.
			return dialContext(ctx, network, net.JoinHostPort(item.podIP, item.podPort))
		}}

	ok, err := prober.Do(
		item.podState.context,
		transport,
		item.url,
		prober.WithHeader(network.ProbeHeaderName, network.ProbeHeaderValue),
		prober.ExpectsStatusCodes([]int{http.StatusOK}),
		prober.ExpectsHeader(network.HashHeaderName, item.ingressState.hash))

	// In case of cancellation, drop the work item
	select {
	case <-item.podState.context.Done():
		m.workQueue.Forget(obj)
		return true
	default:
	}

	if err != nil || !ok {
		// In case of error, enqueue for retry
		m.workQueue.AddRateLimited(obj)
		m.logger.Errorf("Probing of %s failed, IP: %s:%s, ready: %t, error: %v (depth: %d)", item.url, item.podIP, item.podPort, ok, err, m.workQueue.Len())
	} else {
		m.updateStates(item.ingressState, item.podState)
	}
	return true
}

func (m *Prober) updateStates(ingressState *ingressState, podState *podState) {
	if atomic.AddInt32(&podState.successCount, 1) == 1 {
		// This is the first successful probe call for the pod, cancel all other work items for this pod
		podState.cancel()

		// This is the last pod being successfully probed, the Ingress is ready
		if atomic.AddInt32(&ingressState.pendingCount, -1) == 0 {
			m.readyCallback(ingressState.ia)
		}
	}
}
