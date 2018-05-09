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

// RevisionActivator is a component that changes revision serving status
// to active if necessary. Then it returns the endpoint once the revision
// is ready to serve traffic.
type RevisionActivator struct {
	kubeClient kubernetes.Interface
	elaClient  clientset.Interface
}

// NewRevisionActivator creates and starts a new RevisionActivator.
func NewRevisionActivator(kubeClient kubernetes.Interface, elaClient clientset.Interface) Activator {
	return Activator(
		&RevisionActivator{
			kubeClient: kubeClient,
			elaClient:  elaClient,
		},
	)
}

func (r *RevisionActivator) ActiveEndpoint(namespace, name string) (end Endpoint, status Status, activationError error) {
	rev := revisionId{namespace: namespace, name: name}

	internalError := func(msg string, args ...interface{}) {
		log.Printf(msg, args...)
		activationError = fmt.Errorf(msg, args...)
		status = http.StatusInternalServerError
	}

	// Get the current revision serving state
	revisionClient := r.elaClient.ElafrosV1alpha1().Revisions(rev.namespace)
	revision, err := revisionClient.Get(rev.name, metav1.GetOptions{})
	if err != nil {
		internalError("Unable to get revision %s/%s: %v", rev.namespace, rev.name, err)
		return
	}
	switch revision.Spec.ServingState {
	default:
		internalError("Disregarding activation request for revision %s/%s in unknown state %v",
			rev.namespace, rev.name, revision.Spec.ServingState)
		return
	case v1alpha1.RevisionServingStateRetired:
		msg := fmt.Sprintf("Disregarding activation request for retired revision %s/%s", rev.namespace, rev.name)
		log.Printf(msg)
		return end, http.StatusPreconditionFailed, fmt.Errorf(msg)
	case v1alpha1.RevisionServingStateActive:
		// Revision is already active. Nothing to do
	case v1alpha1.RevisionServingStateReserve:
		// Activate the revision
		revision.Spec.ServingState = v1alpha1.RevisionServingStateActive
		_, err := revisionClient.Update(revision)
		if err != nil {
			internalError("Failed to activate revision %s/%s: %v", rev.namespace, rev.name, err)
			return
		}
		log.Printf("Activated revision %s/%s", rev.namespace, rev.name)
	}

	// Wait for the revision to be ready
	if !revision.Status.IsReady() {
		wi, err := r.elaClient.ElafrosV1alpha1().Revisions(rev.namespace).Watch(metav1.ListOptions{
			FieldSelector: fmt.Sprintf("metadata.name=%s", rev.name),
		})
		if err != nil {
			internalError("Failed to watch the revision %s/%s", rev.namespace, rev.name)
			return
		}
		defer wi.Stop()
		ch := wi.ResultChan()
	RevisionReady:
		for {
			select {
			case event := <-ch:
				if revision, ok := event.Object.(*v1alpha1.Revision); ok {
					if !revision.Status.IsReady() {
						log.Printf("Revision %s/%s is not yet ready", rev.namespace, rev.name)
						continue
					}
					break RevisionReady
				} else {
					internalError("Unexpected result type for revision %s/%s: %v", rev.namespace, rev.name, event)
					return
				}
			case <-time.After(60 * time.Second):
				internalError("Timeout waiting for revision %s/%s to become ready", rev.namespace, rev.name)
				return
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
		internalError("Unable to get endpoint %s for revision %s/%s: %v",
			endpointName, rev.namespace, rev.name, err)
		return
	}
	if len(k8sEndpoint.Subsets) != 1 {
		internalError("Revision %s/%s needs one endpoint subset. Found %v", rev.namespace, rev.name, len(k8sEndpoint.Subsets))
		return
	}
	subset := k8sEndpoint.Subsets[0]
	if len(subset.Addresses) != 1 || len(subset.Ports) != 1 {
		internalError("Revision %s/%s needs one endpoint address and port. Found %v addresses and %v ports",
			rev.namespace, rev.name, len(subset.Addresses), len(subset.Ports))
		return
	}
	ip := subset.Addresses[0].IP
	port := subset.Ports[0].Port
	if ip == "" || port == 0 {
		internalError("Invalid ip %q or port %q for revision %s/%s", ip, port, rev.namespace, rev.name)
		return
	}

	// Return the endpoint and active=true
	end = Endpoint{
		Ip:   ip,
		Port: port,
	}
	return
}
