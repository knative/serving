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

package ingress

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"k8s.io/apimachinery/pkg/util/wait"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"knative.dev/pkg/apis/istio/v1alpha3"
	"knative.dev/serving/pkg/network/prober"
	"knative.dev/serving/pkg/reconciler/ingress/resources"

	corev1listers "k8s.io/client-go/listers/core/v1"
	istiolisters "knative.dev/pkg/client/listers/istio/v1alpha3"
)

const (
	// probeConcurrency defines how many probing calls can be issued simultaneously
	probeConcurrency = 15
	// stateExpiration defines how long after being last accessed a state expires
	stateExpiration = 5 * time.Minute
	// cleanupPeriod defines how often states are cleaned up
	cleanupPeriod = 1 * time.Minute
)

type probingState struct {
	// probeHost is the value of the HTTP 'Host' header sent when probing
	probeHost      string
	virtualService *v1alpha3.VirtualService

	// pendingCount is the number of pods that haven't been successfully probed yet
	pendingCount int32
	lastAccessed time.Time

	context context.Context
	cancel  func()
}

type workItem struct {
	*probingState
	podIP string
}

// StatusManager provides a way to check if a VirtualService is ready
type StatusManager interface {
	IsReady(vs *v1alpha3.VirtualService) (bool, error)
}

// StatusProber provides a way to check if a VirtualService is ready by probing the Envoy pods
// handling that VirtualService.
type StatusProber struct {
	logger *zap.SugaredLogger

	// mu guards probingStates
	mu            sync.Mutex
	probingStates map[string]*probingState

	workQueue workqueue.RateLimitingInterface

	gatewayLister    istiolisters.GatewayLister
	podLister        corev1listers.PodLister
	transportFactory func() http.RoundTripper

	readyCallback func(*v1alpha3.VirtualService)

	probeConcurrency int
	stateExpiration  time.Duration
	cleanupPeriod    time.Duration
}

// NewStatusProber creates a new instance of StatusProber
func NewStatusProber(
	logger *zap.SugaredLogger,
	gatewayLister istiolisters.GatewayLister,
	podLister corev1listers.PodLister,
	transportFactory func() http.RoundTripper,
	readyCallback func(*v1alpha3.VirtualService)) *StatusProber {
	return &StatusProber{
		logger:        logger,
		probingStates: make(map[string]*probingState),
		workQueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"ProbingQueue"),
		gatewayLister:    gatewayLister,
		podLister:        podLister,
		transportFactory: transportFactory,
		readyCallback:    readyCallback,
		probeConcurrency: probeConcurrency,
		stateExpiration:  stateExpiration,
		cleanupPeriod:    cleanupPeriod,
	}
}

// IsReady checks if the provided VirtualService is ready, i.e. the Envoy pods serving the VirtualService
// have all been updated. This function is designed to be used by the ClusterIngress controller, i.e. it
// will be called in the order of reconciliation. This means that if IsReady is called on a VirtualService,
// this VirtualService is the latest known version and therefore anything related to older versions can be ignored.
// Also, it means that IsReady is not called concurrently.
func (m *StatusProber) IsReady(vs *v1alpha3.VirtualService) (bool, error) {
	key := makeKey(vs)

	// Find the probe host
	var probeHost string
	for _, host := range vs.Spec.Hosts {
		if strings.HasSuffix(host, resources.ProbeHostSuffix) {
			if probeHost != "" {
				return false, fmt.Errorf("only one probe host can be defined in a VirtualService, found at least 2: %s, %s", probeHost, host)
			}
			probeHost = host
		}
	}
	if probeHost == "" {
		m.logger.Errorf("The VirtualService doesn't contain a probe host. Suffix: %q, Hosts: %v", resources.ProbeHostSuffix, vs.Spec.Hosts)
		return false, errors.New("only a VirtualService with a probe host can be probed")
	}

	if ready, ok := func() (bool, bool) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if state, ok := m.probingStates[key]; ok {
			if state.probeHost == probeHost {
				state.lastAccessed = time.Now()
				return atomic.LoadInt32(&state.pendingCount) == 0, true
			}

			// Cancel the polling for the outdated version
			state.cancel()
			delete(m.probingStates, key)
		}
		return false, false
	}(); ok {
		return ready, nil
	}

	podIPs, err := m.listVirtualServicePodIPs(vs)
	if err != nil {
		return false, fmt.Errorf("failed to list the IP addresses of the Pods impacted by the VirtualService: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	state := &probingState{
		virtualService: vs,
		probeHost:      probeHost,
		pendingCount:   int32(len(podIPs)),
		lastAccessed:   time.Now(),
		context:        ctx,
		cancel:         cancel,
	}

	func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.probingStates[key] = state
	}()
	for _, podIP := range podIPs {
		workItem := &workItem{
			probingState: state,
			podIP:        podIP,
		}
		m.workQueue.AddRateLimited(workItem)
	}

	return len(podIPs) == 0, nil
}

// Start starts the StatusManager background operations
func (m *StatusProber) Start(done <-chan struct{}) {
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

// Cancel cancels probing of the provided VirtualService.
func (m *StatusProber) Cancel(vs *v1alpha3.VirtualService) {
	key := makeKey(vs)

	m.mu.Lock()
	defer m.mu.Unlock()
	if state, ok := m.probingStates[key]; ok {
		state.cancel()
		delete(m.probingStates, key)
	}
}

func makeKey(vs *v1alpha3.VirtualService) string {
	return fmt.Sprintf("%s/%s", vs.Namespace, vs.Name)
}

// expireOldStates removes the states that haven't been accessed in a while.
func (m *StatusProber) expireOldStates() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, state := range m.probingStates {
		if time.Since(state.lastAccessed) > m.stateExpiration {
			state.cancel()
			delete(m.probingStates, key)
		}
	}
}

// processWorkItem processes a single work item from workQueue.
// It returns false when there is no more items to process, true otherwise.
func (m *StatusProber) processWorkItem() bool {
	obj, shutdown := m.workQueue.Get()
	if shutdown {
		return false
	}

	defer m.workQueue.Done(obj)

	// Crash if the item is not of the expected type
	item, ok := obj.(*workItem)
	if !ok {
		m.logger.Fatalf("Unexpected work item type: want: %s, got: %s\n", "*workItem", reflect.TypeOf(obj).Name())
	}

	ok, err := prober.Do(
		item.context,
		m.transportFactory(),
		fmt.Sprintf("http://%s/", item.podIP),
		prober.WithHost(item.probeHost),
		prober.ExpectsStatusCodes([]int{http.StatusOK, http.StatusMovedPermanently}),
	)

	// In case of cancellation, drop the work item
	select {
	case <-item.context.Done():
		m.workQueue.Forget(obj)
		return true
	default:
	}

	if err != nil || !ok {
		// In case of error, enqueue for retry
		m.workQueue.AddRateLimited(obj)
		m.logger.Errorf("Probing of %s failed: ready: %t, error: %v", item.podIP, ok, err)
	} else {
		// In case of success, update the state
		if atomic.AddInt32(&item.pendingCount, -1) == 0 {
			m.readyCallback(item.virtualService)
		}
	}
	return true
}

// listVirtualServicePodIPs lists the IP addresses of the Envoy pods impacted by the provided VirtualService.
func (m *StatusProber) listVirtualServicePodIPs(vs *v1alpha3.VirtualService) ([]string, error) {
	var podIPs []string
	for _, name := range vs.Spec.Gateways {
		// Only the ingress gateways are probed
		if name == "mesh" {
			continue
		}

		// Gateway is either "namespace/gateway" or "gateway" (implicitly inside the VirtualService's namespace)
		namespace, name, err := cache.SplitMetaNamespaceKey(name)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Gateway name %q: %v", name, err)
		}
		if namespace == "" {
			namespace = vs.Namespace
		}
		gateway, err := m.gatewayLister.Gateways(namespace).Get(name)
		if err != nil {
			return nil, fmt.Errorf("failed to get Gateway %s/%s: %v", namespace, name, err)
		}

		// Skip gateways that cannot be probed
		if !resources.CanProbeGateway(gateway) {
			continue
		}

		// List matching Pods
		selector := labels.NewSelector()
		for key, value := range gateway.Spec.Selector {
			requirement, err := labels.NewRequirement(key, selection.Equals, []string{value})
			if err != nil {
				return nil, fmt.Errorf("failed to create 'Equals' requirement from %q=%q: %v", key, value, err)
			}
			selector = selector.Add(*requirement)
		}
		pods, err := m.podLister.List(selector)
		if err != nil {
			return nil, fmt.Errorf("failed to list Pods: %v", err)
		}

		// Filter out the Pods without an assigned IP address
		for _, pod := range pods {
			if pod.Status.PodIP != "" {
				podIPs = append(podIPs, pod.Status.PodIP)
			}
		}
	}
	return podIPs, nil
}
