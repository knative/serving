/*
Copyright 2018 The Knative Authors

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
package activator

import (
	"fmt"
	"sync"

	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var shuttingDownError = activationResult{
	endpoint: Endpoint{},
	status:   Status(500),
	err:      fmt.Errorf("activator shutting down"),
}

type activationResult struct {
	endpoint Endpoint
	status   Status
	err      error
}

var _ Activator = (*dedupingActivator)(nil)

type dedupingActivator struct {
	mux             sync.Mutex
	pendingRequests map[revisionID][]chan activationResult
	activator       Activator
	shutdown        bool
	knaClient       clientset.Interface
	logger          *zap.SugaredLogger
	reporter        StatsReporter
}

// NewDedupingActivator creates an Activator that deduplicates
// activations requests for the same revision id and namespace.
func NewDedupingActivator(a Activator, knaClient clientset.Interface, logger *zap.SugaredLogger, r StatsReporter) Activator {
	return &dedupingActivator{
		pendingRequests: make(map[revisionID][]chan activationResult),
		activator:       a,
		knaClient:       knaClient,
		logger:          logger,
		reporter:        r,
	}
}

func (a *dedupingActivator) ActiveEndpoint(namespace, configuration, name string) (Endpoint, Status, error) {
	id := revisionID{namespace: namespace,
		configuration: configuration,
		name:          name}
	ch := make(chan activationResult, 1)
	a.dedupe(id, ch)
	result := <-ch
	return result.endpoint, result.status, result.err
}

func (a *dedupingActivator) Shutdown() {
	a.activator.Shutdown()
	a.mux.Lock()
	defer a.mux.Unlock()
	a.shutdown = true
	for _, reqs := range a.pendingRequests {
		for _, ch := range reqs {
			ch <- shuttingDownError
		}
	}
}

func (a *dedupingActivator) dedupe(id revisionID, ch chan activationResult) {
	a.mux.Lock()
	defer a.mux.Unlock()
	if a.shutdown {
		ch <- shuttingDownError
		return
	}
	if reqs, ok := a.pendingRequests[id]; ok {
		a.pendingRequests[id] = append(reqs, ch)
	} else {
		a.pendingRequests[id] = []chan activationResult{ch}
		go a.activate(id)
	}
}

func (a *dedupingActivator) activate(id revisionID) {
	logger := loggerWithRevisionInfo(a.logger, id.namespace, id.name)
	a.mux.Lock()
	defer a.mux.Unlock()
	if reqs, ok := a.pendingRequests[id]; ok {
		if err := a.reportRequests(id, len(reqs)); err != nil {
			logger.Errorf("Failed to report request count metrics for revision %s for namespace %s", id.name, id.namespace)
		}
	}
	endpoint, status, err := a.activator.ActiveEndpoint(id.namespace, id.configuration, id.name)
	result := activationResult{
		endpoint: endpoint,
		status:   status,
		err:      err,
	}
	if reqs, ok := a.pendingRequests[id]; ok {
		delete(a.pendingRequests, id)
		for _, ch := range reqs {
			ch <- result
		}
	}
}

func (a *dedupingActivator) reportRequests(id revisionID, count int) error {
	logger := loggerWithRevisionInfo(a.logger, id.namespace, id.name)
	revisionClient := a.knaClient.ServingV1alpha1().Revisions(id.namespace)
	revision, err := revisionClient.Get(id.name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("Unable to get revision %s for namespace: %s", id.name, id.namespace)
	}
	a.reporter.ReportRequest(id.namespace, id.configuration, id.name, string(revision.Spec.ServingState), RequestCountM, float64(count))
	logger.Infof("Wrote request_count metric for revision %s for namespace %s with value %d", id.name, id.namespace, count)
	return nil
}
