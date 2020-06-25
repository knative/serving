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
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"

	fakepodsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/pod/fake"

	"knative.dev/pkg/controller"
	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/resources"

	. "knative.dev/pkg/reconciler/testing"
)

var (
	testStats = []Stat{{
		PodName:                          "pod-1",
		AverageConcurrentRequests:        3.0,
		AverageProxiedConcurrentRequests: 2.0,
		RequestCount:                     5,
		ProxiedRequestCount:              4,
	}, {
		PodName:                          "pod-2",
		AverageConcurrentRequests:        5.0,
		AverageProxiedConcurrentRequests: 4.0,
		RequestCount:                     7,
		ProxiedRequestCount:              6,
	}, {
		PodName:                          "pod-3",
		AverageConcurrentRequests:        3.0,
		AverageProxiedConcurrentRequests: 2.0,
		RequestCount:                     5,
		ProxiedRequestCount:              4,
	}}
)

const (
	testRevision  = "just-a-test-revision"
	testNamespace = "putting-fun-into-namespaces"
)

// testStatsWithTime will generate n Stats, each stat having
// pod start time spaced 10s in the past more than the previous one.
// Each pod will return `(i+1)*2`, as it's `AverageConcurrentRequests`.
func testStatsWithTime(n int, youngestSecs float64) []Stat {
	ret := make([]Stat, 0, n)
	tmpl := Stat{
		AverageProxiedConcurrentRequests: 3.0,
		RequestCount:                     2,
		ProxiedRequestCount:              4,
	}
	for i := 0; i < n; i++ {
		s := tmpl
		s.PodName = "pod-" + strconv.Itoa(i)
		s.AverageConcurrentRequests = float64((i + 1) * 2)
		s.ProcessUptime = float64(i*10) + youngestSecs
		ret = append(ret, s)
	}

	return ret
}

func TestNewServiceScraperWithClientHappyCase(t *testing.T) {
	metric := testMetric()
	ctx, cancel, _ := SetupFakeContextWithCancel(t)
	t.Cleanup(cancel)
	accessor := resources.NewPodAccessor(
		fakepodsinformer.Get(ctx).Lister(),
		testNamespace, testRevision)
	sc, err := NewStatsScraper(metric, accessor, logtesting.TestLogger(t))
	if err != nil {
		t.Fatal("NewServiceScraper =", err)
	}
	if svcS, want := sc.(*serviceScraper), urlFromTarget(testRevision+"-zhudex", testNamespace); svcS.url != want {
		t.Errorf("scraper.url = %s, want: %s", svcS.url, want)
	}
}

func checkBaseStat(t *testing.T, got Stat) {
	if got.PodName != scraperPodName {
		t.Errorf("stat.PodName=%v, want %v", got.PodName, scraperPodName)
	}
	// (3.0 + 5.0 + 3.0) / 3.0 * 3 = 11
	if got.AverageConcurrentRequests != 11.0 {
		t.Errorf("stat.AverageConcurrentRequests=%v, want %v",
			got.AverageConcurrentRequests, 11.0)
	}
	// ((5 + 7 + 5) / 3.0) * 3 = 17
	if got.RequestCount != 17 {
		t.Errorf("stat.RequestCount=%v, want %v", got.RequestCount, 17)
	}
	// (2.0 + 4.0 + 2.0) / 3.0 * 3 = 8
	if got.AverageProxiedConcurrentRequests != 8.0 {
		t.Errorf("stat.AverageProxiedConcurrentRequests=%v, want %v",
			got.AverageProxiedConcurrentRequests, 8.0)
	}
	// ((4 + 6 + 4) / 3.0) * 3 = 14
	if got.ProxiedRequestCount != 14 {
		t.Errorf("stat.ProxiedRequestCount=%v, want %v", got.ProxiedRequestCount, 14)
	}
}

func TestNewServiceScraperWithClientErrorCases(t *testing.T) {
	invalidMetric := testMetric()
	invalidMetric.Labels = map[string]string{}
	client := newTestScrapeClient(testStats, []error{nil})
	ctx, cancel, _ := SetupFakeContextWithCancel(t)
	t.Cleanup(cancel)
	podAccessor := resources.NewPodAccessor(
		fakepodsinformer.Get(ctx).Lister(),
		testNamespace, testRevision)
	logger := logtesting.TestLogger(t)

	testCases := []struct {
		name        string
		metric      *av1alpha1.Metric
		client      scrapeClient
		counter     resources.EndpointsCounter
		accessor    resources.PodAccessor
		expectedErr string
	}{{
		name:        "Empty Decider",
		client:      client,
		accessor:    podAccessor,
		expectedErr: "metric must not be nil",
	}, {
		name:        "Missing revision label in Decider",
		metric:      invalidMetric,
		client:      client,
		accessor:    podAccessor,
		expectedErr: "no Revision label found for Metric " + testRevision,
	}}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := newServiceScraperWithClient(test.metric,
				test.accessor, test.client, test.client, logger); err != nil {
				got := err.Error()
				want := test.expectedErr
				if got != want {
					t.Errorf("Got error message: %v. Want: %v", got, want)
				}
			} else {
				t.Error("Expected error from CreateNewServiceScraper, got nil")
			}
		})
	}
}

func TestPodDirectScrapeSuccess(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		cancel()
		t.Fatal("StartInformers() =", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, 3)

	client := newTestScrapeClient(testStats, []error{nil})
	scraper, err := serviceScraperForTest(ctx, t, client, nil /* mesh not used */, true)
	if err != nil {
		t.Fatal("serviceScraperForTest:", err)
	}

	if _, err := scraper.Scrape(defaultMetric.Spec.StableWindow); err != nil {
		t.Fatal("Unexpected error from scraper.Scrape():", err)
	}

	if !scraper.podsAddressable {
		t.Error("PodAddressable switched to false")
	}
}

func TestPodDirectScrapeSomeFailButSuccess(t *testing.T) {
	// For 5 pods, we need 4 successes.
	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		cancel()
		t.Fatal("StartInformers() =", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, 5)

	client := newTestScrapeClient(testStats, []error{nil, nil, errors.New("okay"), nil, nil})
	scraper, err := serviceScraperForTest(ctx, t, client, nil /* mesh not used */, true)
	if err != nil {
		t.Fatal("serviceScraperForTest:", err)
	}
	got, err := scraper.Scrape(defaultMetric.Spec.StableWindow)
	if err != nil {
		t.Fatal("Unexpected error from scraper.Scrape():", err)
	}

	// Checking one of the metrics is enough here.
	if got.AverageConcurrentRequests != 20.0 {
		t.Errorf("stat.AverageConcurrentRequests=%v, want %v",
			got.AverageConcurrentRequests, 20.0)
	}

	if !scraper.podsAddressable {
		t.Error("PodAddressable switched to false")
	}
}

func TestPodDirectScrapeNoneSucceed(t *testing.T) {
	testStats := testStatsWithTime(4, youngPodCutOffDuration.Seconds() /*youngest*/)
	direct := newTestScrapeClient(testStats, []error{
		// Pods fail.
		errors.New("okay"), errors.New("okay"), errors.New("okay"), errors.New("okay"),
	})
	mesh := newTestScrapeClient(testStats, []error{
		// Service succeeds.
		nil, nil, nil, nil,
	})
	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		cancel()
		t.Fatal("StartInformers() =", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, 4)

	scraper, err := serviceScraperForTest(ctx, t, direct, mesh, true)
	if err != nil {
		t.Fatal("serviceScraperForTest:", err)
	}
	got, err := scraper.Scrape(defaultMetric.Spec.StableWindow)
	if err != nil {
		t.Fatal("Unexpected error from scraper.Scrape():", err)
	}

	// Checking one of the metrics is enough here.
	if got.AverageConcurrentRequests != 20.0 {
		t.Errorf("stat.AverageConcurrentRequests=%v, want %v",
			got.AverageConcurrentRequests, 20.0)
	}

	if scraper.podsAddressable {
		t.Error("PodAddressable didn't switch to false")
	}
}

func TestPodDirectScrapePodsExhausted(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		cancel()
		t.Fatal("StartInformers() =", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, 4)

	client := newTestScrapeClient(testStats, []error{nil, nil, errors.New("okay"), nil})
	scraper, err := serviceScraperForTest(ctx, t, client, nil /* mesh not used */, true)
	if err != nil {
		t.Fatal("serviceScraperForTest:", err)
	}
	_, err = scraper.Scrape(defaultMetric.Spec.StableWindow)
	if err == nil {
		t.Fatal("Expected an error")
	}

	if !scraper.podsAddressable {
		t.Error("PodAddressable switched to false")
	}
}

func TestScrapeReportStatWhenAllCallsSucceed(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		cancel()
		t.Fatal("StartInformers() =", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, 3)

	// Scrape will set a timestamp bigger than this.
	client := newTestScrapeClient(testStats, []error{nil})
	scraper, err := serviceScraperForTest(ctx, t, client, client, false)
	if err != nil {
		t.Fatal("serviceScraperForTest:", err)
	}

	got, err := scraper.Scrape(defaultMetric.Spec.StableWindow)
	if err != nil {
		t.Fatal("Unexpected error from scraper.Scrape():", err)
	}

	checkBaseStat(t, got)
}

var youngPodCutOffDuration = defaultMetric.Spec.StableWindow

func TestScrapeAllPodsYoungPods(t *testing.T) {
	const numP = 4
	// All the pods will have life span of less than  `youngPodCutoffSecs`.
	// Also number of pods is greater than minSampleSizeToConsiderAge, so
	// every pod will be scraped twice, before its stat will be considered
	// acceptable.
	testStats := testStatsWithTime(numP, 0. /*youngest*/)

	direct := newTestScrapeClient(testStats, []error{errNoPodsScraped}) // fall back to service scrape
	mesh := newTestScrapeClient(testStats, []error{nil})

	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		cancel()
		t.Fatal("StartInformers() =", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, numP)

	scraper, err := serviceScraperForTest(ctx, t, direct, mesh, false)
	if err != nil {
		t.Fatalf("serviceScraperForTest=%v, want no error", err)
	}
	got, err := scraper.Scrape(defaultMetric.Spec.StableWindow)
	if err != nil {
		t.Fatal("Unexpected error from scraper.Scrape():", err)
	}

	if got.PodName != scraperPodName {
		t.Errorf("stat.PodName=%v, want %v", got.PodName, scraperPodName)
	}
	// (2+4+6+8) / 4.0 * 4 = 20
	if got, want := got.AverageConcurrentRequests, 20.0; got != want {
		t.Errorf("stat.AverageConcurrentRequests=%v, want %v", got, want)
	}
}

func TestScrapeAllPodsOldPods(t *testing.T) {
	const numP = 6
	// All pods are at least cutoff time old, so first 5 stats will be picked.
	testStats := testStatsWithTime(numP, youngPodCutOffDuration.Seconds() /*youngest*/)

	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		cancel()
		t.Fatal("StartInformers() =", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, numP)
	direct := newTestScrapeClient(testStats, []error{errNoPodsScraped}) // fall back to service scrape
	mesh := newTestScrapeClient(testStats, []error{nil})
	scraper, err := serviceScraperForTest(ctx, t, direct, mesh, false)
	if err != nil {
		t.Fatalf("serviceScraperForTest=%v, want no error", err)
	}
	got, err := scraper.Scrape(defaultMetric.Spec.StableWindow)
	if err != nil {
		t.Fatal("Unexpected error from scraper.Scrape():", err)
	}

	if got.PodName != scraperPodName {
		t.Errorf("stat.PodName=%v, want %v", got.PodName, scraperPodName)
	}
	// (2+4+6+8+10) / 5.0 * 6 = 36
	if got, want := got.AverageConcurrentRequests, 36.0; got != want {
		t.Errorf("stat.AverageConcurrentRequests=%v, want %v", got, want)
	}
}

func TestScrapeSomePodsOldPods(t *testing.T) {
	const numP = 11
	// All pods starting with pod-3 qualify.
	// So pods 3-10 qualify (for 11 total sample is 7).
	testStats := testStatsWithTime(numP, youngPodCutOffDuration.Seconds()/2 /*youngest*/)

	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		cancel()
		t.Fatal("StartInformers() =", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, numP)

	// Scrape will set a timestamp bigger than this.
	direct := newTestScrapeClient(testStats, []error{errNoPodsScraped}) // fall back to service scrape
	mesh := newTestScrapeClient(testStats, []error{nil})
	scraper, err := serviceScraperForTest(ctx, t, direct, mesh, false)
	if err != nil {
		t.Fatalf("serviceScraperForTest=%v, want no error", err)
	}

	got, err := scraper.Scrape(defaultMetric.Spec.StableWindow)
	if err != nil {
		t.Fatal("Unexpected error from scraper.Scrape():", err)
	}

	if got.PodName != scraperPodName {
		t.Errorf("stat.PodName=%v, want %v", got.PodName, scraperPodName)
	}
	// (8+10+12+14+16+18+20)=98; 98/7*11 = 154
	if got, want := got.AverageConcurrentRequests, 154.0; got != want {
		t.Errorf("stat.AverageConcurrentRequests=%v, want %v", got, want)
	}
}

func TestScrapeReportErrorCannotFindEnoughPods(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		cancel()
		t.Fatal("StartInformers() =", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, 2)

	client := newTestScrapeClient(testStats[2:], []error{nil})
	scraper, err := serviceScraperForTest(ctx, t, client, client, false)
	if err != nil {
		t.Fatalf("serviceScraperForTest=%v, want no error", err)
	}

	_, err = scraper.Scrape(defaultMetric.Spec.StableWindow)
	if err == nil {
		t.Error("scrape.Scrape() = nil, expected an error")
	}
}

func TestScrapeReportErrorIfAnyFails(t *testing.T) {
	errTest := errors.New("test")

	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		cancel()
		t.Fatal("StartInformers() =", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, 2)

	// 1 success and 10 failures so one scrape fails permanently through retries.
	client := newTestScrapeClient(testStats, []error{nil, errTest, errTest,
		errTest, errTest, errTest, errTest, errTest, errTest, errTest, errTest})
	scraper, err := serviceScraperForTest(ctx, t, client, client, false)
	if err != nil {
		t.Fatalf("serviceScraperForTest=%v, want no error", err)
	}

	_, err = scraper.Scrape(defaultMetric.Spec.StableWindow)
	if !errors.Is(err, errTest) {
		t.Errorf("scraper.Scrape() = %v, want %v wrapped", err, errTest)
	}
}

func TestScrapeDoNotScrapeIfNoPodsFound(t *testing.T) {
	ctx, cancel, _ := SetupFakeContextWithCancel(t)
	t.Cleanup(cancel)
	client := newTestScrapeClient(testStats, nil)
	scraper, err := serviceScraperForTest(ctx, t, client, client, false)
	if err != nil {
		t.Fatalf("serviceScraperForTest=%v, want no error", err)
	}

	stat, err := scraper.Scrape(defaultMetric.Spec.StableWindow)
	if err != nil {
		t.Fatal("scraper.Scrape() returned error:", err)
	}
	if stat != emptyStat {
		t.Error("Received unexpected Stat.")
	}
}

func serviceScraperForTest(ctx context.Context, t *testing.T, directClient, meshClient scrapeClient, podsAddressable bool) (*serviceScraper, error) {
	metric := testMetric()
	accessor := resources.NewPodAccessor(
		fakepodsinformer.Get(ctx).Lister(),
		testNamespace, testRevision)
	logger := logtesting.TestLogger(t)
	ss, err := newServiceScraperWithClient(metric, accessor, directClient, meshClient, logger)
	if ss != nil {
		ss.podsAddressable = podsAddressable
	}
	return ss, err
}

func testMetric() *av1alpha1.Metric {
	return &av1alpha1.Metric{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRevision,
			Labels: map[string]string{
				serving.RevisionLabelKey: testRevision,
			},
		},
		Spec: av1alpha1.MetricSpec{
			StableWindow: time.Minute,
			ScrapeTarget: testRevision + "-zhudex",
		},
	}
}

func newTestScrapeClient(stats []Stat, errs []error) scrapeClient {
	return &fakeScrapeClient{
		stats: stats,
		errs:  errs,
	}
}

type fakeScrapeClient struct {
	curIdx int
	stats  []Stat
	errs   []error
	mutex  sync.Mutex
}

// Scrape return the next item in the stats and error array of fakeScrapeClient.
func (c *fakeScrapeClient) Scrape(url string) (Stat, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	ans := c.stats[c.curIdx%len(c.stats)]
	err := c.errs[c.curIdx%len(c.errs)]
	c.curIdx++
	return ans, err
}

func TestURLFromTarget(t *testing.T) {
	if got, want := "http://dance.now:9090/metrics", urlFromTarget("dance", "now"); got != want {
		t.Errorf("urlFromTarget = %s, want: %s, diff: %s", got, want, cmp.Diff(got, want))
	}
}

func makePods(ctx context.Context, n int) {
	for i := 0; i < n; i++ {
		p := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-" + strconv.Itoa(i),
				Namespace: testNamespace,
				Labels:    map[string]string{serving.RevisionLabelKey: testRevision},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				PodIP: "1.2.3." + strconv.Itoa(4+i),
				Conditions: []corev1.PodCondition{{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				}},
			},
		}

		fakekubeclient.Get(ctx).CoreV1().Pods(testNamespace).Create(p)
		fakepodsinformer.Get(ctx).Informer().GetIndexer().Add(p)
	}
}
