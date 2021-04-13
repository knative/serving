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
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"

	fakepodsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/pod/fake"

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
	sc := NewStatsScraper(metric, testRevision, accessor, false, logtesting.TestLogger(t))
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

func TestPodDirectScrapeSuccess(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("Failed to start informers:", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})

	client := newTestScrapeClient(testStats, []error{nil})
	scraper := serviceScraperForTest(ctx, t, client, nil /* mesh not used */, true /*podsAddressable*/, false /*passthroughLb*/)

	// No pods at all.
	if stat, err := scraper.Scrape(defaultMetric.Spec.StableWindow); err != nil {
		t.Error("Unexpected error from scraper.Scrape():", err)
	} else if !cmp.Equal(stat, emptyStat) {
		t.Errorf("Wanted empty stat got: %#v", stat)
	}

	makePods(ctx, "pods-", 3, metav1.Now())
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
	wf, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("Failed to start informers:", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, "pods-", 5, metav1.Now())

	client := newTestScrapeClient(testStats, []error{nil, nil, errors.New("okay"), nil, nil})
	scraper := serviceScraperForTest(ctx, t, client, nil /* mesh not used */, true /*podsAddressable*/, false /*passthroughLb*/)
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

func TestPodDirectScrapeAllFailWithMeshError(t *testing.T) {
	testStats := testStatsWithTime(4, youngPodCutOffDuration.Seconds() /*youngest*/)
	meshErr := errorWithStatusCode{
		error:      errors.New("just meshing with you"),
		statusCode: 503,
	}
	direct := newTestScrapeClient(testStats, []error{
		// Pods fail, and they're all potentially mesh-related errors.
		meshErr, meshErr, meshErr, meshErr,
	})
	mesh := newTestScrapeClient(testStats, []error{
		// Service succeeds.
		nil, nil, nil, nil,
	})
	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("Failed to start informers:", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, "pods-", 4, metav1.Now())

	scraper := serviceScraperForTest(ctx, t, direct, mesh, true /*podsAddressable*/, false /*passthroughLb*/)
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

func TestPodDirectScrapeSomeFailWithNonMeshError(t *testing.T) {
	meshErr := errorWithStatusCode{
		error:      errors.New("what a mesh"),
		statusCode: 503,
	}
	nonMeshErr := errorWithStatusCode{
		error:      errors.New("cant mesh with this"),
		statusCode: 404,
	}

	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("RunInformers() =", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, "pods-", 4, metav1.Now())

	client := newTestScrapeClient(testStats, []error{meshErr, nonMeshErr, meshErr, meshErr})
	scraper := serviceScraperForTest(ctx, t, client, nil /* mesh not used */, true /*podsAddressable*/, false /*passthroughLb*/)
	_, err = scraper.Scrape(defaultMetric.Spec.StableWindow)
	if err == nil {
		t.Fatal("Expected an error")
	}

	if !scraper.podsAddressable {
		t.Error("PodAddressable switched to false")
	}
}

func TestPodDirectScrapePodsExhausted(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("Failed to start informers:", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, "pods-", 4, metav1.Now())

	client := newTestScrapeClient(testStats, []error{nil, nil, errors.New("okay"), nil})
	scraper := serviceScraperForTest(ctx, t, client, nil /* mesh not used */, true /*podsAddressable*/, false /*passthroughLb*/)
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
	wf, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("Failed to start informers:", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, "pods-", 3, metav1.Now())

	// Scrape will set a timestamp bigger than this.
	client := newTestScrapeClient(testStats, []error{nil})
	scraper := serviceScraperForTest(ctx, t, client, client, false /*podsAddressable*/, false /*passthroughLb*/)

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

	direct := newTestScrapeClient(testStats, []error{errDirectScrapingNotAvailable}) // fall back to service scrape
	mesh := newTestScrapeClient(testStats, []error{nil})

	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("Failed to start informers:", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, "pods-", numP, metav1.Now())

	scraper := serviceScraperForTest(ctx, t, direct, mesh, false /*podsAddressable*/, false /*passthroughLb*/)
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
	wf, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("Failed to start informers:", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, "pods-", numP, metav1.Now())
	direct := newTestScrapeClient(testStats, []error{errDirectScrapingNotAvailable}) // fall back to service scrape
	mesh := newTestScrapeClient(testStats, []error{nil})
	scraper := serviceScraperForTest(ctx, t, direct, mesh, false /*podsAddressable*/, false /*passthroughLb*/)
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
	wf, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("Failed to start informers:", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, "pods-", numP, metav1.Now())

	// Scrape will set a timestamp bigger than this.
	direct := newTestScrapeClient(testStats, []error{errDirectScrapingNotAvailable}) // fall back to service scrape
	mesh := newTestScrapeClient(testStats, []error{nil})
	scraper := serviceScraperForTest(ctx, t, direct, mesh, false /*podsAddressable*/, false /*passthroughLb*/)

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
	wf, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("Failed to start informers:", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, "pods-", 2, metav1.Now())

	client := newTestScrapeClient(testStats[2:], []error{nil})
	scraper := serviceScraperForTest(ctx, t, client, client, false /*podsAddressable*/, false /*passthroughLb*/)

	_, err = scraper.Scrape(defaultMetric.Spec.StableWindow)
	if err == nil {
		t.Error("scrape.Scrape() = nil, expected an error")
	}
}

func TestScrapeReportErrorIfAnyFails(t *testing.T) {
	errTest := errors.New("test")

	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("Failed to start informers:", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, "pods-", 2, metav1.Now())

	// 1 success and 10 failures so one scrape fails permanently through retries.
	client := newTestScrapeClient(testStats, []error{nil, errTest, errTest,
		errTest, errTest, errTest, errTest, errTest, errTest, errTest, errTest})
	scraper := serviceScraperForTest(ctx, t, client, client, false /*podsAddressable*/, false /*passthroughLb*/)

	_, err = scraper.Scrape(defaultMetric.Spec.StableWindow)
	if !errors.Is(err, errTest) {
		t.Errorf("scraper.Scrape() = %v, want %v wrapped", err, errTest)
	}
}

func TestScrapeDoNotScrapeIfNoPodsFound(t *testing.T) {
	ctx, cancel, _ := SetupFakeContextWithCancel(t)
	t.Cleanup(cancel)
	client := newTestScrapeClient(testStats, nil)
	scraper := serviceScraperForTest(ctx, t, client, client, false /*podsAddressable*/, false /*passthroughLb*/)

	stat, err := scraper.Scrape(defaultMetric.Spec.StableWindow)
	if err != nil {
		t.Fatal("scraper.Scrape() returned error:", err)
	}
	if stat != emptyStat {
		t.Error("Received unexpected Stat.")
	}
}

func TestMixedPodShuffle(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("Failed to start informers:", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})

	tm := metav1.NewTime(time.Now().Add(-time.Hour))
	const (
		numPods = 25
		oldPods = 5
	)
	makePods(ctx, "old-", oldPods, tm)
	makePods(ctx, "new-", numPods-oldPods, metav1.Now())
	wantScrapes := int(populationMeanSampleSize(numPods))
	t.Log("WantScrapes", wantScrapes)

	client := newTestScrapeClient(testStats, []error{nil})
	scraper := serviceScraperForTest(ctx, t, client, client, true /*podsAddressable*/, false /*passthroughLb*/)

	_, err = scraper.Scrape(defaultMetric.Spec.StableWindow)
	if err != nil {
		t.Fatal("scraper.Scrape() returned error:", err)
	}
	if got, want := len(client.urls), wantScrapes; got != want {
		t.Fatalf("Got = %d unique URLS, want: %d", got, want)
	}

	// Ensure all the old pods are there.
	cnt := 0
	for s := range client.urls {
		t.Log(s)
		if strings.Contains(s, "old-") {
			cnt++
		}
	}
	if got, want := cnt, oldPods; got != want {
		t.Errorf("Number of scraped old pods = %d, want: %d", got, want)
	}
}

func TestOldPodShuffle(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("Failed to start informers:", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})

	tm := metav1.NewTime(time.Now().Add(-time.Hour))
	const numPods = 30
	makePods(ctx, "old-", 25, tm)
	makePods(ctx, "young-", numPods-25, metav1.Now())
	wantScrapes := int(populationMeanSampleSize(numPods))
	t.Log("WantScrapes", wantScrapes)

	client := newTestScrapeClient(testStats, []error{nil})
	scraper := serviceScraperForTest(ctx, t, client, client, true /*podsAddressable*/, false /*passthroughLb*/)

	_, err = scraper.Scrape(defaultMetric.Spec.StableWindow)
	if err != nil {
		t.Fatal("scraper.Scrape() returned error:", err)
	}
	if got, want := len(client.urls), wantScrapes; got != want {
		t.Fatalf("Got = %d unique URLS, want: %d", got, want)
	}
	// Store and reset.
	firstRun := client.urls
	client.urls = sets.NewString()

	_, err = scraper.Scrape(defaultMetric.Spec.StableWindow)
	if err != nil {
		t.Fatal("scraper.Scrape() returned error:", err)
	}
	if got, want := len(client.urls), wantScrapes; got != want {
		t.Fatalf("Got = %d unique URLS, want: %d", got, want)
	}

	// Verify we shuffled.
	// This might fail every `3268760` runs.
	if firstRun.Equal(client.urls) {
		t.Error("The same set of URLs was scraped both times")
	}

	// Ensure all the old pods are there.
	cnt := 0
	for s := range client.urls {
		t.Log(s)
		if strings.Contains(s, "old-") {
			cnt++
		}
	}
	if got, want := cnt, wantScrapes; got != want {
		t.Errorf("Number of scraped old pods = %d, want: %d", got, want)
	}
}

func TestOldPodsFallback(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("Failed to start informers:", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})

	tm := metav1.NewTime(time.Now().Add(-time.Hour))
	const (
		numPods = 30
		oldPods = 11
	)
	makePods(ctx, "old-", oldPods, tm) // If all succeeded this would've covered.
	makePods(ctx, "young-", numPods-oldPods, metav1.Now())
	wantScrapes := int(populationMeanSampleSize(numPods))
	t.Log("WantScrapes", wantScrapes)

	client := newTestScrapeClient(testStats, func() []error {
		r := make([]error, numPods)
		// This will fail all the old pods.
		for i := 0; i < oldPods; i++ {
			r[i] = errors.New("bad-hair-day")
		}
		// But succeed all the youngs.
		for i := oldPods; i < numPods; i++ {
			r[i] = nil
		}
		return r
	}())
	scraper := serviceScraperForTest(ctx, t, client, client, true /*podsAddressable*/, false /*passthroughLb*/)

	_, err = scraper.Scrape(defaultMetric.Spec.StableWindow)
	if err != nil {
		t.Fatal("scraper.Scrape() returned error:", err)
	}
	if got, want := len(client.urls), wantScrapes*2; got != want {
		t.Fatalf("Got = %d unique URLS, want: %d", got, want)
	}

	// Ensure all the old pods are there.
	ocnt, ycnt := 0, 0
	for s := range client.urls {
		if strings.Contains(s, "old-") {
			ocnt++
		} else {
			ycnt++
		}
	}
	if got, want := ocnt, wantScrapes; got != want {
		t.Fatalf("Number of scraped old pods = %d, want: %d", got, want)
	}
	if got, want := ycnt, wantScrapes; got != want {
		t.Errorf("Number of scraped young pods = %d, want: %d", got, want)
	}
}

func TestPodDirectPassthroughScrapeSuccess(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t)
	wf, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("Failed to start informers:", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})

	client := newTestScrapeClient(testStats, []error{nil})
	scraper := serviceScraperForTest(ctx, t, client, nil /* mesh not used */, true /*podsAddressable*/, true /*passthroughLb*/)

	makePods(ctx, "pods-", 3, metav1.Now())
	if _, err := scraper.Scrape(defaultMetric.Spec.StableWindow); err != nil {
		t.Fatal("Unexpected error from scraper.Scrape():", err)
	}

	if !scraper.podsAddressable {
		t.Error("PodAddressable switched to false")
	}
}

func TestPodDirectPassthroughScrapeNoneSucceed(t *testing.T) {
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
	wf, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("Failed to start informers:", err)
	}
	t.Cleanup(func() {
		cancel()
		wf()
	})
	makePods(ctx, "pods-", 4, metav1.Now())

	scraper := serviceScraperForTest(ctx, t, direct, mesh, true /*podsAddressable*/, true /*passthroughLb*/)

	// No fallback to service scraping expected.
	if _, err = scraper.Scrape(defaultMetric.Spec.StableWindow); err == nil {
		t.Fatal("Expected an error")
	}

	if !scraper.podsAddressable {
		t.Error("PodAddressable switched to false")
	}
}

func serviceScraperForTest(ctx context.Context, t *testing.T, directClient, meshClient scrapeClient,
	podsAddressable bool, usePassthroughLb bool) *serviceScraper {
	metric := testMetric()
	accessor := resources.NewPodAccessor(
		fakepodsinformer.Get(ctx).Lister(),
		testNamespace, testRevision)
	logger := logtesting.TestLogger(t)
	ss := newServiceScraperWithClient(metric, testRevision, accessor, usePassthroughLb, directClient, meshClient, logger)
	ss.podsAddressable = podsAddressable
	return ss
}

func testMetric() *autoscalingv1alpha1.Metric {
	return &autoscalingv1alpha1.Metric{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRevision,
			Labels: map[string]string{
				serving.RevisionLabelKey: testRevision,
			},
		},
		Spec: autoscalingv1alpha1.MetricSpec{
			StableWindow: time.Minute,
			ScrapeTarget: testRevision + "-zhudex",
		},
	}
}

func newTestScrapeClient(stats []Stat, errs []error) *fakeScrapeClient {
	return &fakeScrapeClient{
		stats: stats,
		errs:  errs,
		urls:  sets.NewString(),
	}
}

type fakeScrapeClient struct {
	curIdx int
	stats  []Stat
	errs   []error
	urls   sets.String
	mutex  sync.Mutex
}

// Scrape return the next item in the stats and error array of fakeScrapeClient.
func (c *fakeScrapeClient) Do(req *http.Request) (Stat, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	ans := c.stats[c.curIdx%len(c.stats)]
	err := c.errs[c.curIdx%len(c.errs)]
	c.curIdx++
	c.urls.Insert(req.URL.String())
	return ans, err
}

func TestURLFromTarget(t *testing.T) {
	if got, want := "http://dance.now:9090/metrics", urlFromTarget("dance", "now"); got != want {
		t.Errorf("urlFromTarget = %s, want: %s, diff: %s", got, want, cmp.Diff(got, want))
	}
}

func makePods(ctx context.Context, prefix string, n int, startTime metav1.Time) {
	for i := 0; i < n; i++ {
		p := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      prefix + strconv.Itoa(i),
				Namespace: testNamespace,
				Labels:    map[string]string{serving.RevisionLabelKey: testRevision},
			},
			Status: corev1.PodStatus{
				StartTime: &startTime,
				Phase:     corev1.PodRunning,
				PodIP:     prefix + "1.2.3." + strconv.Itoa(4+i), // no longer a real IP, but ¯\_(ツ)_/¯.
				Conditions: []corev1.PodCondition{{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				}},
			},
		}

		fakekubeclient.Get(ctx).CoreV1().Pods(testNamespace).Create(ctx, p, metav1.CreateOptions{})
		fakepodsinformer.Get(ctx).Informer().GetIndexer().Add(p)
	}
}
