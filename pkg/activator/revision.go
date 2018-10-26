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
	"net/http"
	"time"

	"github.com/knative/pkg/logging/logkey"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	revisionresources "github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources"
	revisionresourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources/names"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var _ Activator = (*revisionActivator)(nil)

type revisionActivator struct {
	readyTimout time.Duration // for testing
	kubeClient  kubernetes.Interface
	knaClient   clientset.Interface
	logger      *zap.SugaredLogger
	reporter    StatsReporter
}

// NewRevisionActivator creates an Activator that changes revision
// serving status to active if necessary, then returns the endpoint
// once the revision is ready to serve traffic.
func NewRevisionActivator(kubeClient kubernetes.Interface, servingClient clientset.Interface, logger *zap.SugaredLogger, reporter StatsReporter) Activator {
	return &revisionActivator{
		readyTimout: 60 * time.Second,
		kubeClient:  kubeClient,
		knaClient:   servingClient,
		logger:      logger,
		reporter:    reporter,
	}
}

func (r *revisionActivator) Shutdown() {
	// nothing to do
}

func (r *revisionActivator) activateRevision(namespace, name string) (*v1alpha1.Revision, error) {
	key := fmt.Sprintf("%s/%s", namespace, name)
	logger := r.logger.With(zap.String(logkey.Key, key))
	rev := revisionID{
		namespace: namespace,
		name:      name,
	}

	// Get the current revision serving state
	revisionClient := r.knaClient.ServingV1alpha1().Revisions(rev.namespace)
	revision, err := revisionClient.Get(rev.name, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "Unable to get revision")
	}

	serviceName, configurationName := getServiceAndConfigurationLabels(revision)
	r.reporter.ReportRequest(namespace, serviceName, configurationName, name, 1.0)

	// Wait for the revision to be ready
	if !revision.Status.IsReady() {
		wi, err := r.knaClient.ServingV1alpha1().Revisions(rev.namespace).Watch(metav1.ListOptions{
			FieldSelector: fmt.Sprintf("metadata.name=%s", rev.name),
		})
		if err != nil {
			return nil, fmt.Errorf("Failed to watch the revision")
		}
		defer wi.Stop()
		ch := wi.ResultChan()
	RevisionReady:
		for {
			select {
			case <-time.After(r.readyTimout):
				// last chance to check
				if revision.Status.IsReady() {
					break RevisionReady
				}
				return nil, errors.New("Timeout waiting for revision to become ready")
			case event := <-ch:
				if revision, ok := event.Object.(*v1alpha1.Revision); ok {
					if !revision.Status.IsReady() {
						logger.Info("Revision is not yet ready")
						continue
					} else {
						logger.Info("Revision is ready")
					}
					break RevisionReady
				} else {
					return nil, fmt.Errorf("Unexpected result type for revision: %v", event)
				}
			}
		}
	}
	return revision, nil
}

func (r *revisionActivator) getRevisionEndpoint(revision *v1alpha1.Revision) (end Endpoint, err error) {
	// Get the revision endpoint
	services := r.kubeClient.CoreV1().Services(revision.GetNamespace())
	serviceName := revisionresourcenames.K8sService(revision)
	svc, err := services.Get(serviceName, metav1.GetOptions{})
	if err != nil {
		return end, errors.Wrapf(err, "Unable to get service %s for revision", serviceName)
	}

	fqdn := fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, revision.Namespace)

	// Search for the correct port in all the service ports.
	port := int32(-1)
	for _, p := range svc.Spec.Ports {
		if p.Name == revisionresources.ServicePortName {
			port = p.Port
			break
		}
	}
	if port == -1 {
		return end, fmt.Errorf("Revision needs external. Found %v", len(svc.Spec.Ports))
	}

	return Endpoint{
		FQDN: fqdn,
		Port: port,
	}, nil
}

func (r *revisionActivator) ActiveEndpoint(namespace, name string) ActivationResult {
	key := fmt.Sprintf("%s/%s", namespace, name)
	logger := r.logger.With(zap.String(logkey.Key, key))
	revision, err := r.activateRevision(namespace, name)
	if err != nil {
		logger.Error("Failed to activate the revision.", zap.Error(err))
		return ActivationResult{
			Status: http.StatusInternalServerError,
			Error:  err,
		}
	}

	serviceName, configurationName := getServiceAndConfigurationLabels(revision)
	endpoint, err := r.getRevisionEndpoint(revision)
	if err != nil {
		logger.Error("Failed to get revision endpoint.", zap.Error(err))
		return ActivationResult{
			Status:            http.StatusInternalServerError,
			ServiceName:       serviceName,
			ConfigurationName: configurationName,
			Error:             err,
		}
	}

	return ActivationResult{
		Status:            http.StatusOK,
		Endpoint:          endpoint,
		ServiceName:       serviceName,
		ConfigurationName: configurationName,
		Error:             nil,
	}
}

func getServiceAndConfigurationLabels(rev *v1alpha1.Revision) (string, string) {
	if rev.Labels == nil {
		return "", ""
	}
	return rev.Labels[serving.ServiceLabelKey], rev.Labels[serving.ConfigurationLabelKey]
}
