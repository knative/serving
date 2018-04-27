package activator

import (
	"fmt"
	"net/http"
	"time"

	"github.com/cloudflare/cfssl/log"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	clientset "github.com/elafros/elafros/pkg/client/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// RevisionActivator is a component that changes revision serving status
// to active if necessary. Then it returns the endpoint once the revision
// is ready to serve traffic.
type RevisionActivator struct {
	kubeClient         kubernetes.Interface
	elaClient          clientset.Interface
	activationRequests <-chan *RevisionId
	endpoints          chan<- *RevisionEndpoint
}

// NewRevisionActivator creates and starts a new RevisionActivator.
func NewRevisionActivator(
	kubeClient kubernetes.Interface,
	elaClient clientset.Interface,
	activationRequests <-chan *RevisionId,
	endpoints chan<- *RevisionEndpoint,
) *RevisionActivator {
	r := &RevisionActivator{
		kubeClient:         kubeClient,
		elaClient:          elaClient,
		activationRequests: activationRequests,
		endpoints:          endpoints,
	}
	go func() {
		for {
			select {
			case req <- activationRequests:
				go r.activate(req)
			}
		}
	}()
	return r
}

func (r *RevisionActivator) activate(rev *RevisionId) {

	// In all cases, return a RevisionEndpoint with an endpoint or
	// error / status to release pending requests.
	end := &RevisionEndpoint{RevisionId: rev}
	defer func() { r.endpoints <- end }()
	internalError := func(msg string, args ...string) {
		log.Printf(msg, args...)
		end.err = fmt.Errorf(msg, args...)
		end.status = http.StatusInternalServerError
	}

	// Get the current revision serving state
	revisionClient := a.elaClient.ElafrosV1alpha1().Revisions(rev.Namespace)
	revision, err := revisionClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		internalError("Unable to get revision %s/%s: %v", rev.Namespace, rev.Name, err)
		return
	}
	switch revision.Spec.ServingState {
	default:
		internalError("Disregarding activation request for revision %s/%s in unknown state %v",
			rev.Namespace, rev.Name, revision.Spec.ServingState)
		return
	case v1alpha1.RevisionServingStateRetired:
		msg := fmt.Sprintf("Disregarding activation request for retired revision %s/%s", rev.Namespace, rev.Name)
		log.Printf(msg)
		end.err = fmt.Errorf(msg)
		end.status = http.StatusPreconditionFailed
		return
	case v1alpha1.RevisionServingStateActive:
		// Revision is already active. Nothing to do
	case v1alpha1.RevisionServingStateReserve:
		// Activate the revision
		revision.Spec.ServingState = v1alpha1.RevisionStateServingStateActive
		_, err := revisionClient.Update(revision)
		if err != nil {
			internalError("Failed to activate revision %s/%s: %v", rev.Namespace, rev.Name, err)
			return
		}
		log.Printf("Activated revision %s/%s", rev.Namespace, rev.Name)
	}

	// Wait for the revision to be ready
	wi, err := r.elaClient.ElafrosV1alpha1().Revisions(rev.Namespace).Watch(metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", rev.Name),
	})
	if err != nil {
		internalError("Failed to watch the revision %s/%s", rev.Namespace, rev.Name)
		return
	}
	defer wi.Stop()
	ch := wi.ResultChan()
RevisionReady:
	for {
		select {
		case event := <-ch:
			if rev, ok := event.Object.(*v1alpha1.Revision); ok {
				if !rev.Status.IsReady() {
					log.Printf("Revision %s/%s is not yet ready", rev.Namespace, rev.Name)
					continue
				}
				break RevisionReady
			} else {
				internalError("Unexpected result type for revision %s/%s: %v", rev.Namespace, rev.Name, event)
				return
			}
		case <-time.After(60 * time.Second):
			internalError("Timeout waiting for revision %s/%s to become ready", rev.Namespace, rev.Name)
			return
		}
	}

	// Get the revision endpoint
	//
	// TODO: figure out why do we need to use the pod IP directly to avoid the delay.
	// We should be able to use the k8s service cluster IP.
	// https://github.com/elafros/elafros/issues/660
	endpointName := controller.GetElaK8sServiceNameForRevision(revision)
	endpoint, err := r.kubeClient.CoreV1().Endpoints(rev.Namespace).Get(endpointName, metav1.GetOptions{})
	if err != nil {
		internalError("Unable to get endpoint %s for revision %s/%s: %v",
			endpointName, rev.Namespace, rev.Name, err)
		return
	}
	if len(endpoint.Subsets[0].Ports) != 1 {
		internalError("Revision %s/%s needs one endpoint. Found %v ports",
			rev.Namespace, rev.Name, len(endpoint.Subsets[0].Ports))
		return
	}
	ip := endpoint.Subsets[0].IP
	port := endpoint.Subsets[0].Port
	if ip == "" || port == "" {
		internalError("Invalid ip %q or port %q for revision %s/%s", ip, port, rev.Namespace, rev.Name)
		return
	}

	// Return the endpoint and active=true
	end.Endpoint = Endpoint{
		IP:   ip,
		Port: port,
	}
}
