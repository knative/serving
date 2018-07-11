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

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	revisionresourcenames "github.com/knative/serving/pkg/controller/revision/resources/names"
	"github.com/knative/serving/pkg/logging/logkey"
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
}

// NewRevisionActivator creates an Activator that changes revision
// serving status to active if necessary, then returns the endpoint
// once the revision is ready to serve traffic.
func NewRevisionActivator(kubeClient kubernetes.Interface, servingClient clientset.Interface, logger *zap.SugaredLogger) Activator {
	return &revisionActivator{
		readyTimout: 60 * time.Second,
		kubeClient:  kubeClient,
		knaClient:   servingClient,
		logger:      logger,
	}
}

func (r *revisionActivator) Shutdown() {
	// nothing to do
}

func (r *revisionActivator) ActiveEndpoint(namespace, name string) (end Endpoint, status Status, activationError error) {
	logger := loggerWithRevisionInfo(r.logger, namespace, name)
	rev := revisionID{namespace: namespace, name: name}

	internalError := func(msg string, args ...interface{}) (Endpoint, Status, error) {
		logger.Infof(msg, args...)
		return Endpoint{}, http.StatusInternalServerError, fmt.Errorf(fmt.Sprintf("%s for namespace: %s, revision name: %s ", msg, namespace, name), args...)
	}

	// Get the current revision serving state
	revisionClient := r.knaClient.ServingV1alpha1().Revisions(rev.namespace)
	revision, err := revisionClient.Get(rev.name, metav1.GetOptions{})
	if err != nil {
		return internalError("Unable to get revision: %v", err)
	}
	switch revision.Spec.ServingState {
	default:
		return internalError("Disregarding activation request for revision in unknown state %v", revision.Spec.ServingState)
	case v1alpha1.RevisionServingStateRetired:
		return internalError("Disregarding activation request for retired revision ")
	case v1alpha1.RevisionServingStateActive:
		// Revision is already active. Nothing to do
	case v1alpha1.RevisionServingStateReserve:
		// Activate the revision
		revision.Spec.ServingState = v1alpha1.RevisionServingStateActive
		if _, err := revisionClient.Update(revision); err != nil {
			return internalError("Failed to activate revision %v", err)
		}
		logger.Info("Activated revision")
	}

	// Wait for the revision to be ready
	if !revision.Status.IsReady() {
		wi, err := r.knaClient.ServingV1alpha1().Revisions(rev.namespace).Watch(metav1.ListOptions{
			FieldSelector: fmt.Sprintf("metadata.name=%s", rev.name),
		})
		if err != nil {
			return internalError("Failed to watch the revision")
		}
		defer wi.Stop()
		ch := wi.ResultChan()
	RevisionReady:
		for {
			select {
			case <-time.After(r.readyTimout):
				return internalError("Timeout waiting for revision to become ready")
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
					return internalError("Unexpected result type for revision: %v", event)
				}
			}
		}
	}

	// Get the revision endpoint
	services := r.kubeClient.CoreV1().Services(revision.GetNamespace())
	serviceName := revisionresourcenames.K8sService(revision)
	svc, err := services.Get(serviceName, metav1.GetOptions{})
	if err != nil {
		return internalError("Unable to get service %s for revision: %v",
			serviceName, err)
	}

	// TODO: in the future, the target service could have more than one port.
	// https://github.com/knative/serving/issues/837
	if len(svc.Spec.Ports) != 1 {
		return internalError("Revision needs one port. Found %v", len(svc.Spec.Ports))
	}
	fqdn := fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, revision.Namespace)
	port := svc.Spec.Ports[0].Port

	// Return the endpoint and active=true
	end = Endpoint{
		FQDN: fqdn,
		Port: port,
	}
	return end, 0, nil
}

// loggerWithRevisionInfo enriches the logs with revision name and namespace.
func loggerWithRevisionInfo(logger *zap.SugaredLogger, ns string, name string) *zap.SugaredLogger {
	return logger.With(zap.String(logkey.Namespace, ns), zap.String(logkey.Revision, name))
}
