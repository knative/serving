/*
Copyright 2019 The Knative Authors

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

package metrics

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.uber.org/atomic"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	pkgmetrics "knative.dev/pkg/metrics"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/metrics"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/resources"
)

const (
	httpClientTimeout = 3 * time.Second

	// scraperPodName is the name used in all stats sent from the scraper to
	// the autoscaler. The actual customer pods are hidden behind the scraper. The
	// autoscaler does need to know how many customer pods are reporting metrics.
	// Instead, the autoscaler knows the stats it receives are either from the
	// scraper or the activator.
	scraperPodName = "service-scraper"

	// scraperMaxRetries are retries to be done to the actual Scrape routine. We want
	// to retry if a Scrape returns an error or if the Scrape goes to a pod we already
	// scraped.
	scraperMaxRetries = 10
)

var (
	// ErrFailedGetEndpoints specifies the error returned by scraper when it fails to
	// get endpoints.
	ErrFailedGetEndpoints = errors.New("failed to get endpoints")

	// ErrDidNotReceiveStat specifies the error returned by scraper when it does not receive
	// stat from an unscraped pod
	ErrDidNotReceiveStat = errors.New("did not receive stat from an unscraped pod")

	// Sentinel error to return from pod scraping routine, when all pods fail
	// with a 503 error code, indicating (most likely), that mesh is enabled.
	errDirectScrapingNotAvailable = errors.New("all pod scrapes returned 503 error")
	errPodsExhausted              = errors.New("pods exhausted")

	scrapeTimeM = stats.Float64(
		"scrape_time",
		"Time to scrape metrics in milliseconds",
		stats.UnitMilliseconds)
)

func init() {
	if err := view.Register(
		&view.View{
			Description: "The time to scrape metrics in milliseconds",
			Measure:     scrapeTimeM,
			Aggregation: view.Distribution(pkgmetrics.Buckets125(1, 100000)...),
		},
	); err != nil {
		panic(err)
	}
}

// StatsScraper defines the interface for collecting Revision metrics
type StatsScraper interface {
	// Scrape scrapes the Revision queue metric endpoint. The duration is used
	// to cutoff young pods, whose stats might skew lower.
	Scrape(time.Duration) (Stat, error)
}

// scrapeClient defines the interface for collecting Revision metrics for a given
// URL. Internal used only.
type scrapeClient interface {
	// Do executes the given request.
	Do(*http.Request) (Stat, error)
}

// noKeepAliveTransport is a http.Transport with the default settings, but with
// KeepAlive disabled. This is used in the mesh case, where we want to avoid
// getting the same host on each connection.
var noKeepAliveTransport = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableKeepAlives = true
	return t
}()

// keepAliveTransport is a http.Transport with the default settings, but with
// keepAlive upped to allow 1000 connections.
var keepAliveTransport = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableKeepAlives = false // default, but for clarity.
	t.MaxIdleConns = 1000
	return t
}()

// noKeepaliveClient is a http client with HTTP Keep-Alive disabled.
// This client is used in the mesh case since we want to get a new connection -
// and therefore, hopefully, host - on every scrape of the service.
var noKeepaliveClient = &http.Client{
	Transport: noKeepAliveTransport,
	Timeout:   httpClientTimeout,
}

// client is a normal http client with HTTP Keep-Alive enabled.
// This client is used in the direct pod scraping (no mesh) case where we want
// to take advantage of HTTP Keep-Alive to avoid connection creation overhead
// between scrapes of the same pod.
var client = &http.Client{
	Timeout:   httpClientTimeout,
	Transport: keepAliveTransport,
}

// serviceScraper scrapes Revision metrics via a K8S service by sampling. Which
// pod to be picked up to serve the request is decided by K8S. Please see
// https://kubernetes.io/docs/concepts/services-networking/network-policies/
// for details.
type serviceScraper struct {
	directClient scrapeClient
	meshClient   scrapeClient

	host     string
	url      string
	statsCtx context.Context
	logger   *zap.SugaredLogger

	podAccessor      resources.PodAccessor
	usePassthroughLb bool
	podsAddressable  bool
}

// NewStatsScraper creates a new StatsScraper for the Revision which
// the given Metric is responsible for.
func NewStatsScraper(metric *autoscalingv1alpha1.Metric, revisionName string, podAccessor resources.PodAccessor,
	usePassthroughLb bool, logger *zap.SugaredLogger) StatsScraper {
	directClient := newHTTPScrapeClient(client)
	meshClient := newHTTPScrapeClient(noKeepaliveClient)
	return newServiceScraperWithClient(metric, revisionName, podAccessor, usePassthroughLb, directClient, meshClient, logger)
}

func newServiceScraperWithClient(
	metric *autoscalingv1alpha1.Metric,
	revisionName string,
	podAccessor resources.PodAccessor,
	usePassthroughLb bool,
	directClient, meshClient scrapeClient,
	logger *zap.SugaredLogger) *serviceScraper {
	svcName := metric.Labels[serving.ServiceLabelKey]
	cfgName := metric.Labels[serving.ConfigurationLabelKey]

	ctx := metrics.RevisionContext(metric.ObjectMeta.Namespace, svcName, cfgName, revisionName)

	return &serviceScraper{
		directClient:     directClient,
		meshClient:       meshClient,
		host:             metric.Spec.ScrapeTarget + "." + metric.ObjectMeta.Namespace,
		url:              urlFromTarget(metric.Spec.ScrapeTarget, metric.ObjectMeta.Namespace),
		podAccessor:      podAccessor,
		podsAddressable:  true,
		usePassthroughLb: usePassthroughLb,
		statsCtx:         ctx,
		logger:           logger,
	}
}

var portAndPath = strconv.Itoa(networking.AutoscalingQueueMetricsPort) + "/metrics"

func urlFromTarget(t, ns string) string {
	return fmt.Sprintf("http://%s.%s:", t, ns) + portAndPath
}

// Scrape calls the destination service then sends it
// to the given stats channel.
func (s *serviceScraper) Scrape(window time.Duration) (stat Stat, err error) {
	startTime := time.Now()
	defer func() {
		// No errors and an empty stat? We didn't scrape at all because
		// we're scaled to 0.
		if stat == emptyStat && err == nil {
			return
		}
		scrapeTime := time.Since(startTime)
		pkgmetrics.RecordBatch(s.statsCtx, scrapeTimeM.M(float64(scrapeTime.Milliseconds())))
	}()

	if s.podsAddressable || s.usePassthroughLb {
		stat, err := s.scrapePods(window)
		// Return here if some pods were scraped, but not enough or if we're using a
		// passthrough loadbalancer and want no fallback to service-scrape logic.
		if !errors.Is(err, errDirectScrapingNotAvailable) || s.usePassthroughLb {
			return stat, err
		}
		// Else fall back to service scrape.
	}
	readyPodsCount, err := s.podAccessor.ReadyCount()
	if err != nil {
		return emptyStat, ErrFailedGetEndpoints
	}
	if readyPodsCount == 0 {
		return emptyStat, nil
	}
	stat, err = s.scrapeService(window, readyPodsCount)
	if err == nil && s.podsAddressable {
		s.logger.Info("Direct pod scraping off, service scraping, on")
		// If err == nil, this means that we failed to scrape all pods, but service worked
		// thus it is probably a mesh case.
		s.podsAddressable = false
	}
	return stat, err
}

func (s *serviceScraper) scrapePods(window time.Duration) (Stat, error) {
	pods, youngPods, err := s.podAccessor.PodIPsSplitByAge(window, time.Now())
	if err != nil {
		s.logger.Infow("Error querying pods by age", zap.Error(err))
		return emptyStat, err
	}
	lp := len(pods)
	lyp := len(youngPods)
	s.logger.Debugf("|OldPods| = %d, |YoungPods| = %d", lp, lyp)
	total := lp + lyp
	if total == 0 {
		return emptyStat, nil
	}

	frpc := float64(total)
	sampleSizeF := populationMeanSampleSize(frpc)
	sampleSize := int(sampleSizeF)
	results := make(chan Stat, sampleSize)

	// 1. If not enough: shuffle young pods and expect to use N-lp of those
	//		no need to shuffle old pods, since all of them are expected to be used.
	// 2. If enough old pods: shuffle them and use first N, still append young pods
	//		as backup in case of errors, but without shuffling.
	if lp < sampleSize {
		rand.Shuffle(lyp, func(i, j int) {
			youngPods[i], youngPods[j] = youngPods[j], youngPods[i]
		})
	} else {
		rand.Shuffle(lp, func(i, j int) {
			pods[i], pods[j] = pods[j], pods[i]
		})
	}
	pods = append(pods, youngPods...)

	grp, egCtx := errgroup.WithContext(context.Background())
	idx := atomic.NewInt32(-1)
	var sawNonMeshError atomic.Bool
	// Start |sampleSize| threads to scan in parallel.
	for i := 0; i < sampleSize; i++ {
		grp.Go(func() error {
			// If a given pod failed to scrape, we want to continue
			// scanning pods down the line.
			for {
				// Acquire next pod.
				myIdx := int(idx.Inc())
				// All out?
				if myIdx >= len(pods) {
					return errPodsExhausted
				}

				// Scrape!
				target := "http://" + pods[myIdx] + ":" + portAndPath
				req, err := http.NewRequestWithContext(egCtx, http.MethodGet, target, nil)
				if err != nil {
					return err
				}

				if s.usePassthroughLb {
					req.Host = s.host
					req.Header.Add("Knative-Direct-Lb", "true")
				}

				stat, err := s.directClient.Do(req)
				if err == nil {
					results <- stat
					return nil
				}

				if !isPotentialMeshError(err) {
					sawNonMeshError.Store(true)
				}

				s.logger.Infow("Failed scraping pod "+pods[myIdx], zap.Error(err))
			}
		})
	}

	err = grp.Wait()
	close(results)

	// We only get here if one of the scrapers failed to scrape
	// at least one pod.
	if err != nil {
		// Got some (but not enough) successful pods.
		if len(results) > 0 {
			s.logger.Warn("Too many pods failed scraping for meaningful interpolation")
			return emptyStat, errPodsExhausted
		}
		// We didn't get any pods, but we don't want to fall back to service
		// scraping because we saw an error which was not mesh-related.
		if sawNonMeshError.Load() {
			s.logger.Warn("0 pods scraped, but did not see a mesh-related error")
			return emptyStat, errPodsExhausted
		}
		// No pods, and we only saw mesh-related errors, so infer that mesh must be
		// enabled and fall back to service scraping.
		s.logger.Warn("0 pods were successfully scraped out of ", strconv.Itoa(len(pods)))
		return emptyStat, errDirectScrapingNotAvailable
	}

	return computeAverages(results, sampleSizeF, frpc), nil
}

func computeAverages(results <-chan Stat, sample, total float64) Stat {
	ret := Stat{
		PodName: scraperPodName,
	}

	// Sum the stats from individual pods.
	for stat := range results {
		ret.add(stat)
	}

	ret.average(sample, total)
	return ret
}

// scrapeService scrapes the metrics using service endpoint
// as its target, rather than individual pods.
func (s *serviceScraper) scrapeService(window time.Duration, readyPods int) (Stat, error) {
	frpc := float64(readyPods)

	sampleSizeF := populationMeanSampleSize(frpc)
	sampleSize := int(sampleSizeF)
	oldStatCh := make(chan Stat, sampleSize)
	youngStatCh := make(chan Stat, sampleSize)
	scrapedPods := &sync.Map{}

	grp, egCtx := errgroup.WithContext(context.Background())
	youngPodCutOffSecs := window.Seconds()
	for i := 0; i < sampleSize; i++ {
		grp.Go(func() error {
			for tries := 1; ; tries++ {
				stat, err := s.tryScrape(egCtx, scrapedPods)
				if err != nil {
					// Return the error if we exhausted our retries and
					// we had an error returned (we can end up here if
					// all the pods were young, which is not an error condition).
					if tries >= scraperMaxRetries {
						return err
					}
					continue
				}

				if stat.ProcessUptime >= youngPodCutOffSecs {
					// We run |sampleSize| goroutines and each of them terminates
					// as soon as it sees a stat from an `oldPod`.
					// The channel is allocated to |sampleSize|, thus this will never
					// deadlock.
					oldStatCh <- stat
					return nil
				}

				select {
				// This in theory might loop over all the possible pods, thus might
				// fill up the channel.
				case youngStatCh <- stat:
				default:
					// If so, just return.
					return nil
				}
			}
		})
	}

	// Now at this point we have two possibilities.
	// 1. We scraped |sampleSize| distinct pods, with the invariant of
	// 		   sampleSize <= len(oldStatCh) + len(youngStatCh) <= sampleSize*2.
	//    Note, that `err` might still be non-nil, especially when the overall
	//    pod population is small.
	//    Consider the following case: sampleSize=3, in theory the first go routine
	//    might scrape 2 pods, the second 1 and the third won't be be able to scrape
	//		any unseen pod, so it will return `ErrDidNotReceiveStat`.
	// 2. We did not: in this case `err` below will be non-nil.

	// Return the inner error, if any.
	if err := grp.Wait(); err != nil {
		// Ignore the error if we have received enough statistics.
		if !errors.Is(err, ErrDidNotReceiveStat) || len(oldStatCh)+len(youngStatCh) < sampleSize {
			return emptyStat, fmt.Errorf("unsuccessful scrape, sampleSize=%d: %w", sampleSize, err)
		}
	}
	close(oldStatCh)
	close(youngStatCh)

	ret := Stat{
		PodName: scraperPodName,
	}

	// Sum the stats from individual pods.
	oldCnt := len(oldStatCh)
	for stat := range oldStatCh {
		ret.add(stat)
	}
	for i := oldCnt; i < sampleSize; i++ {
		// This will always succeed, see reasoning above.
		ret.add(<-youngStatCh)
	}

	ret.average(sampleSizeF, frpc)
	return ret, nil
}

// tryScrape runs a single scrape and returns stat if this is a pod that has not been
// seen before. An error otherwise or if scraping failed.
func (s *serviceScraper) tryScrape(ctx context.Context, scrapedPods *sync.Map) (Stat, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return emptyStat, err
	}
	stat, err := s.meshClient.Do(req)
	if err != nil {
		return emptyStat, err
	}

	if _, exists := scrapedPods.LoadOrStore(stat.PodName, struct{}{}); exists {
		return emptyStat, ErrDidNotReceiveStat
	}

	return stat, nil
}
