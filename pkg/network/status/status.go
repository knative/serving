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
	"net/url"
	"path"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/time/rate"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"

	"knative.dev/pkg/network/prober"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/network/ingress"
)

const (
	// probeConcurrency defines how many probing calls can be issued simultaneously
	probeConcurrency = 15
	// probeTimeout defines the maximum amount of time a request will wait
	probeTimeout = 1 * time.Second
	probePath    = "/healthz"
	// initialDelay defines the delay before enqueuing a probing request the first time.
	// It gives times for the change to propagate and prevents unnecessary retries.
	initialDelay = 200 * time.Millisecond
)

var dialContext = (&net.Dialer{Timeout: probeTimeout}).DialContext

// ingressState represents the probing state of an Ingress
type ingressState struct {
	hash string
	ing  *v1alpha1.Ingress

	// pendingCount is the number of pods that haven't been successfully probed yet
	pendingCount int32
	lastAccessed time.Time

	cancel func()
}

// podState represents the probing state of a Pod (for a specific Ingress)
type podState struct {
	// successCount is the number of successful probes
	successCount int32

	cancel func()
}

// cancelContext is a pair of a Context and its cancel function
type cancelContext struct {
	context context.Context
	cancel  func()
}

type workItem struct {
	ingressState *ingressState
	podState     *podState
	context      context.Context
	url          *url.URL
	podIP        string
	podPort      string
}

// ProbeTarget contains the URLs to probes for a set of Pod IPs serving out of the same port.
type ProbeTarget struct {
	PodIPs  sets.String
	PodPort string
	Port    string
	URLs    []*url.URL
}

// ProbeTargetLister lists all the targets that requires probing.
type ProbeTargetLister interface {
	// ListProbeTargets returns a list of targets to be probed.
	ListProbeTargets(ctx context.Context, ingress *v1alpha1.Ingress) ([]ProbeTarget, error)
}

// Manager provides a way to check if an Ingress is ready
type Manager interface {
	IsReady(ctx context.Context, ing *v1alpha1.Ingress) (bool, error)
}

// Prober provides a way to check if a VirtualService is ready by probing the Envoy pods
// handling that VirtualService.
type Prober struct {
	logger *zap.SugaredLogger

	// mu guards ingressStates and podContexts
	mu            sync.Mutex
	ingressStates map[string]*ingressState
	podContexts   map[string]cancelContext

	workQueue workqueue.RateLimitingInterface

	targetLister ProbeTargetLister

	readyCallback func(*v1alpha1.Ingress)

	probeConcurrency int
}

// NewProber creates a new instance of Prober
func NewProber(
	logger *zap.SugaredLogger,
	targetLister ProbeTargetLister,
	readyCallback func(*v1alpha1.Ingress)) *Prober {
	return &Prober{
		logger:        logger,
		ingressStates: make(map[string]*ingressState),
		podContexts:   make(map[string]cancelContext),
		workQueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.NewMaxOfRateLimiter(
				// Per item exponential backoff
				workqueue.NewItemExponentialFailureRateLimiter(50*time.Millisecond, 30*time.Second),
				// Global rate limiter
				&workqueue.BucketRateLimiter{Limiter: rate.NewLimiter(rate.Limit(50), 100)},
			),
			"ProbingQueue"),
		targetLister:     targetLister,
		readyCallback:    readyCallback,
		probeConcurrency: probeConcurrency,
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
func (m *Prober) IsReady(ctx context.Context, ing *v1alpha1.Ingress) (bool, error) {
	ingressKey := ingressKey(ing)

	bytes, err := ingress.ComputeHash(ing)
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
		ing:          ing,
		lastAccessed: time.Now(),
		cancel:       cancel,
	}

	// Get the probe targets and group them by IP
	targets, err := m.targetLister.ListProbeTargets(ctx, ing)
	if err != nil {
		return false, err
	}
	workItems := make(map[string][]*workItem)
	for _, target := range targets {
		for ip := range target.PodIPs {
			for _, url := range target.URLs {
				workItems[ip] = append(workItems[ip], &workItem{
					ingressState: ingressState,
					url:          url,
					podIP:        ip,
					podPort:      target.PodPort,
				})
			}
		}
	}

	ingressState.pendingCount = int32(len(workItems))

	for ip, ipWorkItems := range workItems {
		// Get or create the context for that IP
		ipCtx := func() context.Context {
			m.mu.Lock()
			defer m.mu.Unlock()
			cancelCtx, ok := m.podContexts[ip]
			if !ok {
				ctx, cancel := context.WithCancel(context.Background())
				cancelCtx = cancelContext{
					context: ctx,
					cancel:  cancel,
				}
				m.podContexts[ip] = cancelCtx
			}
			return cancelCtx.context
		}()

		podCtx, cancel := context.WithCancel(ingCtx)
		podState := &podState{
			successCount: 0,
			cancel:       cancel,
		}

		// Quick and dirty way to join two contexts (i.e. podCtx is cancelled when either ingCtx or ipCtx are cancelled)
		go func() {
			select {
			case <-podCtx.Done():
				// This is the actual context, there is nothing to do except
				// break to avoid leaking this goroutine.
				break
			case <-ipCtx.Done():
				// Cancel podCtx
				cancel()
			}
		}()

		// Update the states when probing is successful or cancelled
		go func() {
			<-podCtx.Done()
			m.updateStates(ingressState, podState)
		}()

		for _, wi := range ipWorkItems {
			wi.podState = podState
			wi.context = podCtx
			m.workQueue.AddAfter(wi, initialDelay)
			m.logger.Infof("Queuing probe for %s, IP: %s:%s (depth: %d)",
				wi.url, wi.podIP, wi.podPort, m.workQueue.Len())
		}
	}

	func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.ingressStates[ingressKey] = ingressState
	}()
	return len(workItems) == 0, nil
}

// Start starts the Manager background operations
func (m *Prober) Start(done <-chan struct{}) chan struct{} {
	var wg sync.WaitGroup

	// Start the worker goroutines
	for i := 0; i < m.probeConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for m.processWorkItem() {
			}
		}()
	}

	// Stop processing the queue when cancelled
	go func() {
		<-done
		m.workQueue.ShutDown()
	}()

	// Return a channel closed when all work is done
	ch := make(chan struct{})
	go func() {
		wg.Wait()
		close(ch)
	}()
	return ch
}

// CancelIngressProbing cancels probing of the provided Ingress
// TODO(#6270): Use cache.DeletedFinalStateUnknown.
func (m *Prober) CancelIngressProbing(obj interface{}) {
	if ing, ok := obj.(*v1alpha1.Ingress); ok {
		key := ingressKey(ing)

		m.mu.Lock()
		defer m.mu.Unlock()
		if state, ok := m.ingressStates[key]; ok {
			state.cancel()
			delete(m.ingressStates, key)
		}
	}
}

// CancelPodProbing cancels probing of the provided Pod IP.
//
// TODO(#6269): make this cancellation based on Pod x port instead of just Pod.
func (m *Prober) CancelPodProbing(obj interface{}) {
	if pod, ok := obj.(*corev1.Pod); ok {
		m.mu.Lock()
		defer m.mu.Unlock()

		if ctx, ok := m.podContexts[pod.Status.PodIP]; ok {
			ctx.cancel()
			delete(m.podContexts, pod.Status.PodIP)
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
		m.logger.Fatalf("Unexpected work item type: want: %s, got: %s\n",
			reflect.TypeOf(&workItem{}).Name(), reflect.TypeOf(obj).Name())
	}
	m.logger.Infof("Processing probe for %s, IP: %s:%s (depth: %d)",
		item.url, item.podIP, item.podPort, m.workQueue.Len())

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		// We only want to know that the Gateway is configured, not that the configuration is valid.
		// Therefore, we can safely ignore any TLS certificate validation.
		InsecureSkipVerify: true,
	}
	transport.DialContext = func(ctx context.Context, network, addr string) (conn net.Conn, e error) {
		// Requests with the IP as hostname and the Host header set do no pass client-side validation
		// because the HTTP client validates that the hostname (not the Host header) matches the server
		// TLS certificate Common Name or Alternative Names. Therefore, http.Request.URL is set to the
		// hostname and it is substituted it here with the target IP.
		return dialContext(ctx, network, net.JoinHostPort(item.podIP, item.podPort))
	}

	probeURL := deepCopy(item.url)
	probeURL.Path = path.Join(probeURL.Path, probePath)

	ctx, cancel := context.WithTimeout(item.context, probeTimeout)
	defer cancel()
	ok, err := prober.Do(
		ctx,
		transport,
		probeURL.String(),
		prober.WithHeader(network.UserAgentKey, network.IngressReadinessUserAgent),
		prober.WithHeader(network.ProbeHeaderName, network.ProbeHeaderValue),
		m.probeVerifier(item))

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
		m.logger.Errorf("Probing of %s failed, IP: %s:%s, ready: %t, error: %v (depth: %d)",
			item.url, item.podIP, item.podPort, ok, err, m.workQueue.Len())
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
			m.readyCallback(ingressState.ing)
		}
	}
}

func (m *Prober) probeVerifier(item *workItem) prober.Verifier {
	return func(r *http.Response, _ []byte) (bool, error) {
		// In the happy path, the probe request is forwarded to Activator or Queue-Proxy and the response (HTTP 200)
		// contains the "K-Network-Hash" header that can be compared with the expected hash. If the hashes match,
		// probing is successful, if they don't match, a new probe will be sent later.
		// An HTTP 404/503 is expected in the case of the creation of a new Knative service because the rules will
		// not be present in the Envoy config until the new VirtualService is applied.
		// No information can be extracted from any other scenario (e.g. HTTP 302), therefore in that case,
		// probing is assumed to be successful because it is better to say that an Ingress is Ready before it
		// actually is Ready than never marking it as Ready. It is best effort.
		switch r.StatusCode {
		case http.StatusOK:
			hash := r.Header.Get(network.HashHeaderName)
			switch hash {
			case "":
				m.logger.Errorf("Probing of %s abandoned, IP: %s:%s: the response doesn't contain the %q header",
					item.url, item.podIP, item.podPort, network.HashHeaderName)
				return true, nil
			case item.ingressState.hash:
				return true, nil
			default:
				return false, fmt.Errorf("unexpected hash: want %q, got %q", item.ingressState.hash, hash)
			}
		case http.StatusNotFound, http.StatusServiceUnavailable:
			return false, fmt.Errorf("unexpected status code: want %v, got %v", http.StatusOK, http.StatusNotFound)
		default:
			m.logger.Errorf("Probing of %s abandoned, IP: %s:%s: the response status is %v, expected 200 or 404",
				item.url, item.podIP, item.podPort, r.StatusCode)
			return true, nil
		}
	}
}

// deepCopy copies a URL into a new one
func deepCopy(in *url.URL) *url.URL {
	// Safe to ignore the error since this is a deep copy
	newURL, _ := url.Parse(in.String())
	return newURL
}
