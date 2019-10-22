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
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"knative.dev/pkg/apis/istio/v1alpha3"
	istiolisters "knative.dev/pkg/client/listers/istio/v1alpha3"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/network/prober"
	"knative.dev/serving/pkg/reconciler/ingress/resources"
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
	ia   v1alpha1.IngressAccessor

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

// probeTargets represents the target to probe.
type probeTarget struct {
	// urlTmpl is an url template to probe.
	urlTmpl string
	// targetPort is a port of Gateway pod.
	targetPort string
}

// StatusManager provides a way to check if a VirtualService is ready
type StatusManager interface {
	IsReady(ia v1alpha1.IngressAccessor, gw map[v1alpha1.IngressVisibility]sets.String) (bool, error)
}

// StatusProber provides a way to check if a VirtualService is ready by probing the Envoy pods
// handling that VirtualService.
type StatusProber struct {
	logger *zap.SugaredLogger

	// mu guards ingressStates and podStates
	mu            sync.Mutex
	ingressStates map[string]*ingressState
	podStates     map[string]*podState

	workQueue workqueue.RateLimitingInterface

	gatewayLister   istiolisters.GatewayLister
	endpointsLister corev1listers.EndpointsLister
	serviceLister   corev1listers.ServiceLister

	readyCallback func(v1alpha1.IngressAccessor)

	probeConcurrency int
	stateExpiration  time.Duration
	cleanupPeriod    time.Duration
}

// NewStatusProber creates a new instance of StatusProber
func NewStatusProber(
	logger *zap.SugaredLogger,
	gatewayLister istiolisters.GatewayLister,
	endpointsLister corev1listers.EndpointsLister,
	serviceLister corev1listers.ServiceLister,
	readyCallback func(v1alpha1.IngressAccessor)) *StatusProber {
	return &StatusProber{
		logger:        logger,
		ingressStates: make(map[string]*ingressState),
		podStates:     make(map[string]*podState),
		workQueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"ProbingQueue"),
		gatewayLister:    gatewayLister,
		endpointsLister:  endpointsLister,
		serviceLister:    serviceLister,
		readyCallback:    readyCallback,
		probeConcurrency: probeConcurrency,
		stateExpiration:  stateExpiration,
		cleanupPeriod:    cleanupPeriod,
	}
}

// IsReady checks if the provided IngressAccessor is ready, i.e. the Envoy pods serving the IngressAccessor
// have all been updated. This function is designed to be used by the Ingress controller, i.e. it
// will be called in the order of reconciliation. This means that if IsReady is called on an IngressAccessor,
// this IngressAccessor is the latest known version and therefore anything related to older versions can be ignored.
// Also, it means that IsReady is not called concurrently.
func (m *StatusProber) IsReady(ia v1alpha1.IngressAccessor, gw map[v1alpha1.IngressVisibility]sets.String) (bool, error) {
	ingressKey := fmt.Sprintf("%s/%s", ia.GetNamespace(), ia.GetName())

	bytes, err := resources.ComputeIngressHash(ia)
	if err != nil {
		return false, fmt.Errorf("failed to compute the hash of the IngressAccessor: %w", err)
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
	for gatewayName, hosts := range resources.HostsPerGateway(ia, gw) {
		gateway, err := m.getGateway(gatewayName)
		if err != nil {
			return false, fmt.Errorf("failed to get Gateway %q: %w", gatewayName, err)
		}
		targetsPerPod, err := m.listGatewayTargetsPerPods(gateway)
		if err != nil {
			return false, fmt.Errorf("failed to list the probing URLs of Gateway %q: %w", gatewayName, err)
		}
		if len(targetsPerPod) == 0 {
			continue
		}

		for ip, targets := range targetsPerPod {
			// Each Pod backing a Gateway is probed using the different hosts, protocol and ports until
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

			for _, host := range hosts {
				for _, target := range targets {
					workItem := &workItem{
						ingressState: ingressState,
						podState:     podState,
						url:          fmt.Sprintf(target.urlTmpl, host),
						podIP:        ip,
						podPort:      target.targetPort,
					}
					workItems = append(workItems, workItem)
				}
			}
		}
		ingressState.pendingCount += int32(len(targetsPerPod))
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

// CancelVirtualServiceProbing cancels probing of the provided VirtualService.
func (m *StatusProber) CancelVirtualServiceProbing(vs *v1alpha3.VirtualService) {
	key := fmt.Sprintf("%s/%s", vs.Namespace, vs.Name)

	m.mu.Lock()
	defer m.mu.Unlock()
	if state, ok := m.ingressStates[key]; ok {
		state.cancel()
		delete(m.ingressStates, key)
	}
}

// CancelPodProbing cancels probing of the provided Pod IP.
func (m *StatusProber) CancelPodProbing(pod *corev1.Pod) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state, ok := m.podStates[pod.Status.PodIP]; ok {
		state.cancel()
	}
}

// expireOldStates removes the states that haven't been accessed in a while.
func (m *StatusProber) expireOldStates() {
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
func (m *StatusProber) processWorkItem() bool {
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

func (m *StatusProber) updateStates(ingressState *ingressState, podState *podState) {
	if atomic.AddInt32(&podState.successCount, 1) == 1 {
		// This is the first successful probe call for the pod, cancel all other work items for this pod
		podState.cancel()

		// This is the last pod being successfully probed, the Ingress is ready
		if atomic.AddInt32(&ingressState.pendingCount, -1) == 0 {
			m.readyCallback(ingressState.ia)
		}
	}
}

func (m *StatusProber) getGateway(name string) (*v1alpha3.Gateway, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(name)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Gateway name %q: %w", name, err)
	}
	if namespace == "" {
		return nil, fmt.Errorf("unexpected unqualified Gateway name %q", name)
	}
	return m.gatewayLister.Gateways(namespace).Get(name)
}

// listGatewayPodsURLs returns a map where the keys are the Gateway Pod IPs and the values are the corresponding
// URL templates and the Gateway Pod Port to be probed.
func (m *StatusProber) listGatewayTargetsPerPods(gateway *v1alpha3.Gateway) (map[string][]probeTarget, error) {
	selector := labels.NewSelector()
	for key, value := range gateway.Spec.Selector {
		requirement, err := labels.NewRequirement(key, selection.Equals, []string{value})
		if err != nil {
			return nil, fmt.Errorf("failed to create 'Equals' requirement from %q=%q: %w", key, value, err)
		}
		selector = selector.Add(*requirement)
	}

	services, err := m.serviceLister.Services(gateway.Namespace).List(selector)
	if err != nil {
		return nil, fmt.Errorf("failed to list Services: %w", err)
	}
	if len(services) == 0 {
		m.logger.Infof("Skipping Gateway %s/%s because it has no corresponding Service", gateway.Namespace, gateway.Name)
		return nil, nil
	}
	service := services[0]

	endpoints, err := m.endpointsLister.Endpoints(service.Namespace).Get(service.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get Endpoints: %w", err)
	}

	targetsPerPods := make(map[string][]probeTarget)
	for _, server := range gateway.Spec.Servers {
		var urlTmpl string
		switch server.Port.Protocol {
		case v1alpha3.ProtocolHTTP, v1alpha3.ProtocolHTTP2:
			if server.TLS == nil || !server.TLS.HTTPSRedirect {
				urlTmpl = "http://%%s:%d/"
			}
		case v1alpha3.ProtocolHTTPS:
			urlTmpl = "https://%%s:%d/"
		default:
			m.logger.Infof("Skipping Server %q because protocol %q is not supported", server.Port.Name, server.Port.Protocol)
			continue
		}

		portName, err := findNameForPortNumber(service, int32(server.Port.Number))
		if err != nil {
			m.logger.Infof("Skipping Server %q because Service %s/%s doesn't contain a port %d", server.Port.Name, service.Namespace, service.Name, server.Port.Number)
			continue
		}
		for _, sub := range endpoints.Subsets {
			portNumber, err := findPortNumberForName(sub, portName)
			if err != nil {
				m.logger.Infof("Skipping Subset %v because it doesn't contain a port %q", sub.Addresses, portName)
				continue
			}

			for _, addr := range sub.Addresses {
				targetsPerPods[addr.IP] = append(targetsPerPods[addr.IP], probeTarget{urlTmpl: fmt.Sprintf(urlTmpl, server.Port.Number), targetPort: strconv.Itoa(int(portNumber))})
			}
		}
	}
	return targetsPerPods, nil
}

// findNameForPortNumber finds the name for a given port as defined by a Service.
func findNameForPortNumber(svc *corev1.Service, portNumber int32) (string, error) {
	for _, port := range svc.Spec.Ports {
		if port.Port == portNumber {
			return port.Name, nil
		}
	}
	return "", fmt.Errorf("no port with number %d found", portNumber)
}

// findPortNumberForName resolves a given name to a portNumber as defined by an EndpointSubset.
func findPortNumberForName(sub corev1.EndpointSubset, portName string) (int32, error) {
	for _, subPort := range sub.Ports {
		if subPort.Name == portName {
			return subPort.Port, nil
		}
	}
	return 0, fmt.Errorf("no port for name %q found", portName)
}
