package activator

import (
	"fmt"
	"net/http"

	"github.com/cloudflare/cfssl/log"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	clientset "github.com/elafros/elafros/pkg/client/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// RevisionActivator is a component that makes revisions active and
// returns their endpoint.
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

	// Return a RevisionEndpoint or error / status to release pending requests.
	end := &RevisionEndpoint{RevisionId: rev}
	defer func() { r.endpoints <- end }()

	// Get the revision serving state
	revisionClient := a.elaClient.ElafrosV1alpha1().Revisions(rev.Namespace)
	revision, err := revisionClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		log.Errorf("Unable to get revision %s/%s: %v", rev.Namespace, rev.Name, err)
		end.err = err
		end.status = http.StatusInternalServerError
		return
	}
	switch revision.Spec.ServingState {
	default:
		log.Errorf("Disregarding activation request for revision %s/%s in unknown state %v",
			rev.Namespace, rev.Name, revision.Spec.ServingState)
		return
	case v1alpha1.RevisionServingStateRetired:
		log.Errorf("Disregarding activation request for retired revision %s/%s", rev.Namespace, rev.Name)
		return
	case v1alpha1.RevisionServingStateActive:
		// Nothing to do
	case v1alpha1.RevisionServingStateReserve:
		// Activate the revision
		revision.Spec.ServingState = v1alpha1.RevisionStateServingStateActive
		_, err := revisionClient.Update(revision)
		if err != nil {
			log.Errorf("Failed to activate revision %s/%s: %v", rev.Namespace, rev.Name, err)
			return
		}
		log.Printf("Activated revision %s/%s", rev.Namespace, rev.Name)
	}

	// Wait for the revision to be ready
	wi, err := r.elaClient.ElafrosV1alpha1().Revisions(rev.Namespace).Watch(metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", rev.Name),
	})
	if err != nil {
		log.Errorf("Failed to watch the revision %s/%s", rev.Namespace, rev.Name)
		return
	}
	defer wi.Stop()
	ch := wi.ResultChan()
RevisionReady:
	for {
		event := <-ch
		if rev, ok := event.Object.(*v1alpha1.Revision); ok {
			if !rev.Status.IsReady() {
				log.Printf("Revision %s/%s is not yet ready", rev.Namespace, rev.Name)
				continue
			}
			break RevisionReady
		} else {
			log.Errorf("Unexpected result type for revision %s/%s: %v", rev.Namespace, rev.Name, event)
			return
		}
	}

	// Get the revision endpoint
	endpointName := controller.GetElaK8sServiceNameForRevision(revision)
	endpoint, err := r.kubeClient.CoreV1().Endpoints(rev.Namespace).Get(endpointName, metav1.GetOptions{})
	if err != nil {
		log.Errorf("Unable to get endpoint %s for revision %s/%s: %v",
			endpointName, rev.Namespace, rev.Name, err)
		return
	}
	if len(endpoint.Subsets[0].Ports) != 1 {
		log.Errorf("Revision %s/%s needs one endpoint. Found %v ports",
			rev.Namespace, rev.Name, len(endpoint.Subsets[0].Ports))
		return
	}
	ip := endpoint.Subsets[0].IP
	port := endpoint.Subsets[0].Port
	if ip == "" || port == "" {
		log.Errorf("Invalid ip %q or port %q for revision %s/%s", ip, port, rev.Namespace, rev.Name)
		return
	}

	// Return the endpoint and active=true
	end.Endpoint = Endpoint{
		IP:   ip,
		Port: port,
	}
	end.Active = true
}
