/*
Copyright 2018 Google LLC

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
	"log"
	"net/http"
	"time"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	"github.com/knative/serving/pkg/controller"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var _ Activator = (*revisionActivator)(nil)

type revisionActivator struct {
	readyTimout time.Duration // for testing
	kubeClient  kubernetes.Interface
	knaClient   clientset.Interface
}

// NewRevisionActivator creates an Activator that changes revision
// serving status to active if necessary, then returns the endpoint
// once the revision is ready to serve traffic.
func NewRevisionActivator(kubeClient kubernetes.Interface, elaClient clientset.Interface) Activator {
	return &revisionActivator{
		readyTimout: 60 * time.Second,
		kubeClient:  kubeClient,
		knaClient:   elaClient,
	}
}

func (r *revisionActivator) Shutdown() {
	// nothing to do
}

func (r *revisionActivator) ActiveEndpoint(namespace, name string) (end Endpoint, status Status, activationError error) {
	rev := revisionID{namespace: namespace, name: name}

	internalError := func(msg string, args ...interface{}) (Endpoint, Status, error) {
		log.Printf(msg, args...)
		return Endpoint{}, http.StatusInternalServerError, fmt.Errorf(msg, args...)
	}

	// Get the current revision serving state
	revisionClient := r.knaClient.ServingV1alpha1().Revisions(rev.namespace)
	revision, err := revisionClient.Get(rev.name, metav1.GetOptions{})
	if err != nil {
		return internalError("Unable to get revision %s/%s: %v", rev.namespace, rev.name, err)
	}
	switch revision.Spec.ServingState {
	default:
		return internalError("Disregarding activation request for revision %s/%s in unknown state %v",
			rev.namespace, rev.name, revision.Spec.ServingState)
	case v1alpha1.RevisionServingStateRetired:
		return internalError("Disregarding activation request for retired revision %s/%s", rev.namespace, rev.name)
	case v1alpha1.RevisionServingStateActive:
		// Revision is already active. Nothing to do
	case v1alpha1.RevisionServingStateReserve:
		// Activate the revision
		revision.Spec.ServingState = v1alpha1.RevisionServingStateActive
		if _, err := revisionClient.Update(revision); err != nil {
			return internalError("Failed to activate revision %s/%s: %v", rev.namespace, rev.name, err)
		}
		log.Printf("Activated revision %s/%s", rev.namespace, rev.name)
	}

	// Wait for the revision to be ready
	if !revision.Status.IsReady() {
		wi, err := r.knaClient.ServingV1alpha1().Revisions(rev.namespace).Watch(metav1.ListOptions{
			FieldSelector: fmt.Sprintf("metadata.name=%s", rev.name),
		})
		if err != nil {
			return internalError("Failed to watch the revision %s/%s", rev.namespace, rev.name)
		}
		defer wi.Stop()
		ch := wi.ResultChan()
	RevisionReady:
		for {
			select {
			case <-time.After(r.readyTimout):
				return internalError("Timeout waiting for revision %s/%s to become ready", rev.namespace, rev.name)
			case event := <-ch:
				if revision, ok := event.Object.(*v1alpha1.Revision); ok {
					if !revision.Status.IsReady() {
						log.Printf("Revision %s/%s is not yet ready", rev.namespace, rev.name)
						continue
					} else {
						log.Printf("Revision %s/%s is ready", rev.namespace, rev.name)
					}
					// After a pod goes ready, there is a poll loop to publish that fact, then there are
					// controllers that wake up to propagate the info to each node which configures iptables on
					// a max frequency loop, so it's always possible that there's a small delay.
					// The delay should be O(seconds) max, most of the time.
					// TODO: rely on readinessProbe instead of a hard-coded sleep.
					// https://github.com/elafros/elafros/issues/974
					time.Sleep(2 * time.Second)
					break RevisionReady
				} else {
					return internalError("Unexpected result type for revision %s/%s: %v", rev.namespace, rev.name, event)
				}
			}
		}
	}

	// Get the revision endpoint
	services := r.kubeClient.CoreV1().Services(revision.GetNamespace())
	serviceName := controller.GetElaK8SServiceNameForRevision(revision)
	svc, err := services.Get(serviceName, metav1.GetOptions{})
	if err != nil {
		return internalError("Unable to get service %s for revision %s/%s: %v",
			serviceName, rev.namespace, rev.name, err)
	}

	// TODO: in the future, the target service could have more than one port.
	// https://github.com/elafros/elafros/issues/837
	if len(svc.Spec.Ports) != 1 {
		return internalError("Revision %s/%s needs one port. Found %v", rev.namespace, rev.name, len(svc.Spec.Ports))
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
