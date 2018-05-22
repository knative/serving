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

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	clientset "github.com/elafros/elafros/pkg/client/clientset/versioned"
	"github.com/elafros/elafros/pkg/controller"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var _ Activator = (*revisionActivator)(nil)

type revisionActivator struct {
	readyTimout time.Duration // for testing
	kubeClient  kubernetes.Interface
	elaClient   clientset.Interface
}

// NewRevisionActivator creates an Activator that changes revision
// serving status to active if necessary, then returns the endpoint
// once the revision is ready to serve traffic
func NewRevisionActivator(kubeClient kubernetes.Interface, elaClient clientset.Interface) Activator {
	return &revisionActivator{
		readyTimout: 60 * time.Second,
		kubeClient:  kubeClient,
		elaClient:   elaClient,
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
	revisionClient := r.elaClient.ElafrosV1alpha1().Revisions(rev.namespace)
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
		wi, err := r.elaClient.ElafrosV1alpha1().Revisions(rev.namespace).Watch(metav1.ListOptions{
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
					}
					break RevisionReady
				} else {
					return internalError("Unexpected result type for revision %s/%s: %v", rev.namespace, rev.name, event)
				}
			}
		}
	}

	// Get the revision endpoint
	//
	// TODO: figure out why do we need to use the pod IP directly to avoid the delay.
	// We should be able to use the k8s service cluster IP.
	// https://github.com/elafros/elafros/issues/660
	endpointName := controller.GetElaK8SServiceNameForRevision(revision)
	k8sEndpoint, err := r.kubeClient.CoreV1().Endpoints(rev.namespace).Get(endpointName, metav1.GetOptions{})
	if err != nil {
		return internalError("Unable to get endpoint %s for revision %s/%s: %v",
			endpointName, rev.namespace, rev.name, err)
	}
	if len(k8sEndpoint.Subsets) != 1 {
		return internalError("Revision %s/%s needs one endpoint subset. Found %v", rev.namespace, rev.name, len(k8sEndpoint.Subsets))
	}
	subset := k8sEndpoint.Subsets[0]
	if len(subset.Addresses) != 1 || len(subset.Ports) != 1 {
		return internalError("Revision %s/%s needs one endpoint address and port. Found %v addresses and %v ports",
			rev.namespace, rev.name, len(subset.Addresses), len(subset.Ports))
	}
	ip := subset.Addresses[0].IP
	port := subset.Ports[0].Port
	if ip == "" || port == 0 {
		return internalError("Invalid ip %q or port %q for revision %s/%s", ip, port, rev.namespace, rev.name)
	}

	// Return the endpoint and active=true
	end = Endpoint{
		IP:   ip,
		Port: port,
	}
	return end, 0, nil
}
