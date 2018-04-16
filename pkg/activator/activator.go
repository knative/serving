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
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

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
	chans      Channels
}

// Channels hold all channels for activating revisions.
type Channels struct {
	activateCh        chan (string)
	activationDoneCh  chan (string)
	revisionRequestCh chan (RevisionRequest)
	watchCh           chan (string)
}

// RevisionRequest holds the http request information.
type RevisionRequest struct {
	name      string
	namespace string
	w         http.ResponseWriter
	r         *http.Request
	active    bool
	doneCh    chan (bool)
}

const (
	requestQueueLength = 100
)

// NewActivator returns an Activator.
func NewActivator(kubeClient kubernetes.Interface, elaClient clientset.Interface, tripper http.RoundTripper) (*Activator, error) {
	return &Activator{
		kubeClient: kubeClient,
		elaClient:  elaClient,
		tripper:    tripper,
		chans: Channels{
			activateCh:        make(chan string, requestQueueLength),
			activationDoneCh:  make(chan string, requestQueueLength),
			revisionRequestCh: make(chan RevisionRequest, requestQueueLength),
			watchCh:           make(chan string, requestQueueLength),
		},
	}, nil
}

func getRevisionKey(namespace string, name string) string {
	return namespace + "/" + name
}

func getRevisionNameFromKey(key string) (namespace string, name string, err error) {
	arr := strings.Split(key, "/")
	if len(arr) != 2 {
		glog.Errorf("Invalid revision key ", key)
		return "", "", errors.New("Invalid revision key " + key)
	}
	return arr[0], arr[1], nil
}

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
	return serviceURL, nil
}

func (a *Activator) proxyRequest(revRequest RevisionRequest) {
	revision, err := a.getRevision(revRequest.namespace, revRequest.name)
	serviceURL, err := a.getRevisionTargetURL(revision)
	if err != nil {
		http.Error(revRequest.w, "Failed to forward request.", http.StatusServiceUnavailable)
		return
	}
	glog.Info("Sending a proxy request to ", serviceURL)
	target, err := url.Parse(serviceURL)
	if err != nil {
		glog.Errorf("Failed to parse target URL: %s. Error: %v", serviceURL, err)
		http.Error(revRequest.w, "Failed to forward request.", http.StatusBadRequest)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = http.DefaultTransport.(*http.Transport) //a.tripper
	proxy.ServeHTTP(revRequest.w, revRequest.r)
	// Make sure the handler function exits after ServeHTTP function.
	revRequest.doneCh <- true
	glog.Info("End proxy request")
}

func (a *Activator) proxyRequests(revKey string, requests []RevisionRequest) {
	glog.Infof("Sending %d requests to revision %s.", len(requests), revKey)
	for i := range requests {
		a.proxyRequest(requests[i])
	}
}

func (a *Activator) updateRevision(revision *v1alpha1.Revision) error {
	revisionClient := a.elaClient.ElafrosV1alpha1().Revisions(revision.Namespace)
	_, err := revisionClient.Update(revision)
	if err != nil {
		glog.Errorf("Failed to update the revision: %s/%s", revision.GetNamespace(), revision.GetName())
		return err
	}
	return nil
}

func (a *Activator) getRevisionFromKey(revKey string) (*v1alpha1.Revision, error) {
	ns, name, err := getRevisionNameFromKey(revKey)
	if err != nil {
		return nil, err
	}
	return a.getRevision(ns, name)
}

func (a *Activator) getRevision(ns string, name string) (*v1alpha1.Revision, error) {
	revisionClient := a.elaClient.ElafrosV1alpha1().Revisions(ns)
	revision, err := revisionClient.Get(name, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("Unable to get revision %s/%s", ns, name)
		return nil, err
	}
	return revision, nil
}

func (a *Activator) activate(revKey string) {
	glog.Info("Revision to be activated: ", revKey)
	revision, err := a.getRevisionFromKey(revKey)
	if err != nil {
		return
	}
	revision.Spec.ServingState = v1alpha1.RevisionServingStateActive
	if a.updateRevision(revision) != nil {
		return
	}
	glog.Infof("Updated the revision: %s", revision.GetName())
}

func (a *Activator) watchForReady(revKey string) {
	glog.Infof("Watching for revision %s to be ready", revKey)
	revision, err := a.getRevisionFromKey(revKey)
	if err != nil {
		return
	}
	revisionClient := a.elaClient.ElafrosV1alpha1().Revisions(revision.GetNamespace())
	wi, err := revisionClient.Watch(metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", revision.GetName()),
	})
	if err != nil {
		glog.Errorf("Error when watching the revision. %v", err)
		return
	}
	defer wi.Stop()
	ch := wi.ResultChan()
	for {
		event := <-ch
		if rev, ok := event.Object.(*v1alpha1.Revision); ok {
			if rev.GetName() != revision.GetName() {
				continue
			}
			if !rev.Status.IsReady() {
				continue
			}
			// TODO: Fix RevisionStatus.IsReady()
			time.Sleep(7 * time.Second)
			a.chans.activationDoneCh <- revKey
			glog.Infof("Revision %s is ready.", revKey)
			return
		}
	}
}

// The main method to process requests. Only active or reserved revisions reach here.
func (a *Activator) process() {
	// revisionMap is a map from the revision key to pending requests.
	revisionMap := make(map[string][]RevisionRequest)
	for {
		select {
		case revReq := <-a.chans.revisionRequestCh:
			var revRequests []RevisionRequest
			revKey := getRevisionKey(revReq.namespace, revReq.name)
			if revRequests, ok := revisionMap[revKey]; !ok {
				revRequests = []RevisionRequest{}
				revisionMap[revKey] = revRequests
				// Only put the first reserved revision to the activateCh.
				if !revReq.active {
					glog.Infof("Add %s to activate channel", revKey)
					a.chans.activateCh <- revKey
				}
				// Add a watch for each unique revision
				glog.Infof("Add %s to watch channel", revKey)
				a.chans.watchCh <- revKey
			}
			revisionMap[revKey] = append(revRequests, revReq)
		case revToWatch := <-a.chans.watchCh:
			go a.watchForReady(revToWatch)
		case revToActivate := <-a.chans.activateCh:
			go a.activate(revToActivate)
		case revDone := <-a.chans.activationDoneCh:
			if revRequests, ok := revisionMap[revDone]; ok {
				delete(revisionMap, revDone)
				go a.proxyRequests(revDone, revRequests)
			} else {
				glog.Error("The revision %s is unexpected in activator", revDone)
			}
		}
	}
}

func (a *Activator) handler(w http.ResponseWriter, r *http.Request) {
	// TODO: Use the namespace from the header.
	revisionClient := a.elaClient.ElafrosV1alpha1().Revisions("default")
	revisionName := r.Header.Get(controller.GetRevisionHeaderName())

	revision, err := revisionClient.Get(revisionName, metav1.GetOptions{})
	if err != nil {
		http.Error(w, "Unable to get revision.", http.StatusNotFound)
		return
	}
	glog.Infof("Found revision %s in namespace %s", revision.GetName(), revision.GetNamespace())
	switch revision.Spec.ServingState {
	case v1alpha1.RevisionServingStateActive:
		glog.Infof("The revision %s/%s is active.", revision.GetNamespace(), revision.GetName())
		revRequest := RevisionRequest{
			name:      revision.GetName(),
			namespace: revision.GetNamespace(),
			r:         r,
			w:         w,
			active:    true,
			doneCh:    make(chan bool),
		}
		a.chans.revisionRequestCh <- revRequest
		<-revRequest.doneCh
	case v1alpha1.RevisionServingStateReserve:
		glog.Infof("The revision %s/%s is inactive.", revision.GetNamespace(), revision.GetName())
		revRequest := RevisionRequest{
			name:      revision.GetName(),
			namespace: revision.GetNamespace(),
			r:         r,
			w:         w,
			active:    false,
			doneCh:    make(chan bool),
		}
		a.chans.revisionRequestCh <- revRequest
		<-revRequest.doneCh
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
	go a.process()
	http.HandleFunc("/", a.handler)
	http.ListenAndServe(":8080", nil)
	<-stopCh
	glog.Info("Shutting down Activator")
	return nil
}
