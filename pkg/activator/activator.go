/*
Copyright 2018 Google Inc. All Rights Reserved.
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
	"net/http/httputil"
	"net/url"
	"strconv"

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	clientset "github.com/elafros/elafros/pkg/client/clientset/versioned"
	"github.com/elafros/elafros/pkg/controller"
	"github.com/golang/glog"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Activator that can activate revisions in reserve state or redirect traffic to active revisions.
type Activator struct {
	kubeClient kubernetes.Interface
	elaClient  clientset.Interface
	tripper    http.RoundTripper
}

// NewActivator returns an Activator.
func NewActivator(kubeClient kubernetes.Interface, elaClient clientset.Interface, tripper http.RoundTripper) (*Activator, error) {
	return &Activator{
		kubeClient: kubeClient,
		elaClient:  elaClient,
		tripper:    tripper,
	}, nil
}

func proxyRequest(w http.ResponseWriter, r *http.Request, targetURL string, tripper http.RoundTripper) {
	glog.Info("Sending a proxy request to ", targetURL)
	target, err := url.Parse(targetURL)
	if err != nil {
		glog.Errorf("Failed to parse target URL: %s. Error: %v", targetURL, err)
		http.Error(w, "Failed to forward request.", http.StatusBadRequest)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = tripper
	proxy.ServeHTTP(w, r)
	glog.Info("End proxy request")
}

//getRevisionTargetURL calculates the target URL
func (a *Activator) getRevisionTargetURL(revision *v1alpha1.Revision) (string, error) {
	services := a.kubeClient.CoreV1().Services(revision.GetNamespace())
	svc, err := services.Get(controller.GetElaK8SServiceNameForRevision(revision), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	if len(svc.Spec.Ports) != 1 {
		return "", fmt.Errorf("need just one port. Found %v ports", len(svc.Spec.Ports))
	}
	serviceURL := "http://" + svc.Spec.ClusterIP + ":" + strconv.Itoa(int(svc.Spec.Ports[0].Port))
	glog.Info("serviceURL: ", serviceURL)
	return serviceURL, nil
}

func (a *Activator) updateRevision(revision *v1alpha1.Revision) error {
	revisionClient := a.elaClient.ElafrosV1alpha1().Revisions(revision.Namespace)
	_, err := revisionClient.Update(revision)
	if err != nil {
		glog.Errorf("Failed to update the revision: %s", revision.GetName())
		return err
	}
	glog.Info("Updated the revision: %s", revision.GetName())
	return nil
}

func (a *Activator) handler(w http.ResponseWriter, r *http.Request) {
	// TODO: Use the namespace from the header.
	revisionClient := a.elaClient.ElafrosV1alpha1().Revisions("default")
	revisionName := r.Header.Get("revision")

	glog.Info("Revision name to be activated: ", revisionName)
	revision, err := revisionClient.Get(revisionName, metav1.GetOptions{})
	if err != nil {
		http.Error(w, "Unable to get revision.", http.StatusNotFound)
		return
	}
	glog.Info("Found revision ", revision.GetName())
	glog.Info("Start to proxy request...")
	switch revision.Spec.ServingState {
	case v1alpha1.RevisionServingStateActive:
		// The revision is already active. Forward the request to k8s deployment.
		serviceURL, err := a.getRevisionTargetURL(revision)
		if err != nil {
			http.Error(w, "Failed to forward request.", http.StatusServiceUnavailable)
			return
		}

		glog.Info("The revision is active. Forwarding request to service at ", serviceURL)
		proxyRequest(w, r, serviceURL, a.tripper)
	case v1alpha1.RevisionServingStateReserve:
		// The revision is inactive. Enqueue the request and activate the revision
		glog.Info("the revision is inactive. Activating it and enqueuing the request")
		revision.Spec.ServingState = v1alpha1.RevisionServingStateActive
		if err := a.updateRevision(revision); err != nil {
			http.Error(w, "Unable to update revision.", http.StatusExpectationFailed)
			return
		}

		glog.Info("Waiting for revision to come online")
		if serviceURL, err := a.getRevisionTargetURL(revision); err != nil {
			http.Error(w, "Unable to get service URL of revision.", http.StatusServiceUnavailable)
		} else {
			// TODO: wait for the service to be online.
			glog.Info("Forwarding request to service at ", serviceURL)
			proxyRequest(w, r, serviceURL, a.tripper)
		}
	case v1alpha1.RevisionServingStateRetired:
		glog.Info("revision is retired. do nothing.")
		http.Error(w, "Retired revision.", http.StatusServiceUnavailable)
	default:
		glog.Errorf("unrecognized revision serving status: %s", revision.Spec.ServingState)
		http.Error(w, "Unknown revision status.", http.StatusServiceUnavailable)
	}
}

// Run will set up the event handler for requests.
func (a *Activator) Run(stopCh <-chan struct{}) error {
	glog.Info("Started Activator")

	http.HandleFunc("/", a.handler)
	http.ListenAndServe(":8080", nil)
	<-stopCh
	glog.Info("Shutting down Activator")
	return nil
}
