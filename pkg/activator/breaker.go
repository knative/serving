package activator

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	activatornetwork "github.com/knative/serving/pkg/activator/network"
	"github.com/knative/serving/pkg/apis/networking"
	netlisters "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/network/prober"
	"github.com/knative/serving/pkg/queue"
	"go.uber.org/zap"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

// Breaker is a wrapper around queue.Breaker which probes a revision before allowing breaker concurrency to rise from zero
type Breaker struct {
	*queue.Breaker
	maxCC           int
	logger          *zap.SugaredLogger
	size            int
	cc              int
	replicaCount    int
	isShutdown      bool
	revId           RevisionID
	mux             sync.Mutex
	sizeChangedCond sync.Cond

	sksLister     netlisters.ServerlessServiceLister
	serviceLister corev1listers.ServiceLister
	transport     http.RoundTripper
}

// NewBreaker creates a new Breaker
func NewBreaker(
	logger *zap.SugaredLogger,
	transport http.RoundTripper,
	breakerParams queue.BreakerParams,
	replicaCount int,
	sksLister netlisters.ServerlessServiceLister,
	serviceLister corev1listers.ServiceLister,
	revId RevisionID) *Breaker {
	ret := &Breaker{
		Breaker:      queue.NewBreaker(breakerParams),
		maxCC:        breakerParams.MaxConcurrency,
		logger:       logger,
		size:         0,
		cc:           0,
		replicaCount: replicaCount,
		isShutdown:   false,
		revId:        revId,

		sksLister:     sksLister,
		serviceLister: serviceLister,
		transport:     transport,
	}
	ret.sizeChangedCond.L = &ret.mux

	// Run concurrency updating loop which polls endpoints
	go func() {
		ret.mux.Lock()
		defer ret.mux.Unlock()

		for {
			if !ret.probeAndSetConcurrency() {
				return
			}

			ret.sizeChangedCond.Wait()
		}
	}()

	return ret
}

// Shutdown should be called on a breaker before it is forgotten to exit the probe goroutine
func (b *Breaker) Shutdown() {
	b.mux.Lock()
	defer b.mux.Unlock()
	b.isShutdown = true
	// Break out of pollLoop wait
	b.sizeChangedCond.Broadcast()
}

func (b *Breaker) probeAndSetConcurrency() bool {
	for {
		// If we shutdown already dont wait forever
		if b.isShutdown {
			return false
		}

		// If our size is 0 or were not changing capcity from zero set concurrency, otherwise probe and if successful set concurrency.
		if b.size == 0 || b.Breaker.Capacity() != 0 {
			b.setConcurrency()
			return true
		} else {
			// Release lock while we poll
			b.mux.Unlock()
			err := b.probe()
			b.mux.Lock()

			if err != nil {
				b.logger.Errorw("Failed to probe pod", zap.Error(err))
			} else {
				b.setConcurrency()
				return true
			}

			b.mux.Unlock()
			// wait before we try probing again
			time.Sleep(200 * time.Millisecond)
			b.mux.Lock()
		}
	}
}

// UpdateSize sets the number of endpoint and containerconcurrency for this breaker's revision
func (b *Breaker) UpdateSize(size, cc, replicaCount int) {
	b.mux.Lock()
	defer b.mux.Unlock()

	b.size = size
	b.cc = cc
	b.replicaCount = replicaCount

	b.sizeChangedCond.Broadcast()
}

// minOneOrValue function returns num if its greater than 1
// else the function returns 1
func minOneOrValue(num int) int {
	if num > 1 {
		return num
	}
	return 1
}

func (b *Breaker) setConcurrency() {
	targetCapacity := b.cc * b.size
	if b.size > 0 && (targetCapacity == 0 || targetCapacity > b.maxCC) {
		// The concurrency is unlimited, thus hand out as many tokens as we can in this breaker.
		targetCapacity = b.maxCC
	} else if targetCapacity > 0 {
		targetCapacity = minOneOrValue(targetCapacity / minOneOrValue(b.replicaCount))
	}
	b.logger.Infof("Setting concurrency to %d. cc: %d, size: %d, maxCC: %d", targetCapacity, b.cc, b.size, b.maxCC)
	if err := b.Breaker.UpdateConcurrency(targetCapacity); err != nil {
		b.logger.Errorw("Updating concurrency of breaker", zap.Error(err))
	}
}

func (b *Breaker) probe() error {
	host, err := activatornetwork.PrivateEndpointForRevision(b.revId.Namespace, b.revId.Name, networking.ProtocolHTTP1, b.sksLister, b.serviceLister)
	target := &url.URL{
		Scheme: "http",
		Host:   host,
	}

	ret, err := prober.Do(
		context.Background(),
		b.transport,
		target.String(),
		prober.WithHeader(network.ProbeHeaderName, queue.Name),
		prober.ExpectsBody(queue.Name))
	if err != nil {
		return err
	}
	if !ret {
		return errors.New("Pod probe unsucessful")
	}
	return nil
}
