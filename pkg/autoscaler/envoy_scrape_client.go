package autoscaler

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	corev1lister "k8s.io/client-go/listers/core/v1"
)

type envoyScrapeClient struct {
	httpClient         *http.Client
	metric             *Metric
	podLister          corev1lister.PodLister
	lastScrapedRequest map[string]int64
}

func newEnvoyScrapeClient(httpClient *http.Client, metric *Metric, podLister corev1lister.PodLister) (*envoyScrapeClient, error) {
	if httpClient == nil {
		return nil, errors.New("HTTP client must not be nil")
	}

	return &envoyScrapeClient{
		httpClient:         httpClient,
		metric:             metric,
		podLister:          podLister,
		lastScrapedRequest: map[string]int64{},
	}, nil
}

func (c *envoyScrapeClient) Scrape(url string) (*Stat, error) {
	r, err := labels.NewRequirement("app", selection.Equals, []string{c.metric.Name})
	if err != nil {
		return nil, err
	}
	selector := labels.NewSelector().Add(*r)
	pods, err := c.podLister.Pods(c.metric.Namespace).List(selector)
	if err != nil {
		return nil, err
	}
	var activeRequest, totalRequest, podCount int64
	for _, p := range pods {
		if p.Status.Phase != corev1.PodRunning {
			continue
		}
		scrapeURL := fmt.Sprintf("http://%s:15090/stats/prometheus", p.Status.PodIP)
		req, err := http.NewRequest(http.MethodGet, scrapeURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		scannner := bufio.NewScanner(resp.Body)
		port := c.metric.Labels["container-port"]
		if port == "" {
			for _, con := range p.Spec.Containers {
				if con.Name == c.metric.Name {
					if len(con.Ports) > 0 {
						port = strconv.Itoa(int(con.Ports[0].ContainerPort))
					}
				}
			}
		}
		if port == "" {
			port = "80"
		}

		rqMatchCriteria := fmt.Sprintf("envoy_http_downstream_rq_total{http_conn_manager_prefix=\"%s_%s\"}", p.Status.PodIP, port)
		acrqMatchCriteria := fmt.Sprintf("envoy_http_downstream_rq_active{http_conn_manager_prefix=\"%s_%s\"}", p.Status.PodIP, port)
		for scannner.Scan() {
			if strings.Contains(scannner.Text(), rqMatchCriteria) {
				parts := strings.Split(scannner.Text(), " ")
				if len(parts) == 2 {
					rqs, _ := strconv.Atoi(parts[1])
					totalRequest += int64(rqs)
				}
			}
			if strings.Contains(scannner.Text(), acrqMatchCriteria) {
				parts := strings.Split(scannner.Text(), " ")
				if len(parts) == 2 {
					rqs, _ := strconv.Atoi(parts[1])
					activeRequest += int64(rqs)
				}
			}
		}
		podCount++
	}
	now := time.Now()
	metricKey := fmt.Sprintf("%s/%s", c.metric.Namespace, c.metric.Name)
	stats := &Stat{
		Time:                      &now,
		PodName:                   scraperPodName,
		AverageConcurrentRequests: float64(activeRequest),
		RequestCount:              int32(totalRequest - c.lastScrapedRequest[metricKey]),
	}
	c.lastScrapedRequest[metricKey] = totalRequest
	return stats, nil
}
