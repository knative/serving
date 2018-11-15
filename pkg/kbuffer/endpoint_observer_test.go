package kbuffer

import (
	. "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	"testing"
	"time"
)

var want int
var expect bool
var subsets = 1
const namespace = "default"
const service = "helloworld-service"

func expectToEqual(want, got int, t *testing.T) {
	if want != got {
		t.Errorf("The number of endpoints was not equal to %d after the update", want)
	}
}

func updateAndAssert(before, after *Endpoints, t *testing.T) {
	got := 0
	endpointObserver := NewEndpointObserver()

	endpointObserver.update(before, after)

	got = endpointObserver.endpoints[revisionID{after.Namespace, after.Name}]
	expectToEqual(want, got, t)
}

func filterAndAssert(namespace, service string, e *Endpoints, t *testing.T) {
	endpointObserver := NewEndpointObserver()
	filter := endpointObserver.filter(namespace, service)
	allowed := filter(e)

	if allowed != expect {
		t.Errorf("The filter response wasn't equal to '%t'", expect)
	}
}

// Helper method to create a stub Endpoints object as we receive it from Kube
func genEndpoinstFromKube(namespace, service string, hosts int) Endpoints {
	subsets := genEndpointsSubset(hosts, subsets)
	return Endpoints{ObjectMeta: v1.ObjectMeta{Name: service, Namespace: namespace}, Subsets: subsets}
}

// Helper method to simulate scale up and down of endpoints,
// Provide the number of endpoints before and after to generate the endpoints stub
func genBeforeAfterEndpoints(before, after int, subsets int) (*Endpoints, *Endpoints) {

	subsetsBefore := genEndpointsSubset(before, subsets)
	endpointsBefore := &Endpoints{Subsets: subsetsBefore}

	subsetsAfter := genEndpointsSubset(after, subsets)
	endpointsAfter := &Endpoints{Subsets: subsetsAfter}

	return endpointsBefore, endpointsAfter
}

// Helper method to create a slice of EndpointSubset
func genEndpointsSubset(hostsPerSubset, subsets int) []EndpointSubset {
	resp := []EndpointSubset{}
	if hostsPerSubset > 0 {
		addresses := genAddresses(hostsPerSubset)
		subset := EndpointSubset{Addresses: addresses}
		for s := 0; s < subsets; s++ {
			resp = append(resp, subset)
		}
		return resp
	} else {
		return resp
	}
}

// Helper method to generate a slice of dummy endpoint addresses
func genAddresses(hosts int) (endpoints []EndpointAddress) {
	for i := 0; i < hosts; i++ {
		endpoints = append(endpoints, EndpointAddress{})
	}
	return endpoints
}

// Helper method to emulate endpoint creation and updates via fake kube client
func createAndUpdateEndpoints(hostsBefore, hostsAfter int, kubeClient *fakekubeclientset.Clientset, service string) {
	before := genEndpoinstFromKube(namespace, service, hostsBefore)
	after := genEndpoinstFromKube(namespace, service, hostsAfter)
	_, _ = kubeClient.CoreV1().Endpoints(namespace).Create(&before)
	_, _ = kubeClient.CoreV1().Endpoints(namespace).Update(&after)
}

// Helper method to start endpoint informer
func initEndpointObserver(kubeClient *fakekubeclientset.Clientset) *EndpointObserver{
	ch := make(chan struct{})
	observer := NewEndpointObserver()
	informer := *observer.Start(kubeClient)
	informer.WaitForCacheSync(ch)
	return observer
}

// Helper method to assert the endpoints host metadata is updated in the internal map
func assertEndpoints(want int, revId revisionID, observer *EndpointObserver, t *testing.T) {
	time.Sleep(200 * time.Millisecond)
	got, _ := observer.Get(revId)

	expectToEqual(want, got, t)
}

// Actual tests start from here
// Testing the _update_ function - scale up and scale down
func TestEndpointObserver_Update_Scale_From_Zero_To_One(t *testing.T) {
	want = 1
	before, after := genBeforeAfterEndpoints(0, 1, subsets)
	updateAndAssert(before, after, t)
}

func TestEndpointObserver_Update_Scale_From_Zero_To_Two(t *testing.T) {
	want = 2
	before, after := genBeforeAfterEndpoints(0, 2, subsets)
	updateAndAssert(before, after, t)
}

func TestEndpointObserver_Update_Scale_From_Two_To_One(t *testing.T) {
	want = 1
	before, after := genBeforeAfterEndpoints(2, 1, subsets)
	updateAndAssert(before, after, t)
}

func TestEndpointObserver_Update_Endpoints_Not_Changed(t *testing.T) {
	want = 0
	before, after := genBeforeAfterEndpoints(0, 0, subsets)
	updateAndAssert(before, after, t)
}

func TestEndpointObserver_Update_Several_Subsets_Added(t *testing.T) {
	want = 2
	subsets = 2
	before, after := genBeforeAfterEndpoints(0, 1, subsets)
	updateAndAssert(before, after, t)
}

// Testing the _filter_ function here
// The service we received an update is the same as we are filtering upon, so it should be allowed - true
func TestEndpointsObserver_Filter_Allow(t *testing.T) {
	expect = true

	e := genEndpoinstFromKube(namespace, service, 1)

	filterAndAssert(namespace, service, &e, t)
}

// The service we received an update digresses from the one we are filtering upon, so it should be denied - false
func TestEndpointsObserver_Filter_Forbid(t *testing.T) {
	expect = false
	allowedService := "a-service"
	notAllowedService := "b-service"

	e := genEndpoinstFromKube(namespace, notAllowedService, 1)

	filterAndAssert(namespace, allowedService, &e, t)
}

// Testing the _Watch_ function here
// Make sure the underlying informer is correctly initialized and event handler is enabled
func TestEndpointsObserver_Watch_Scale_Out(t *testing.T) {
	subsets = 1
	want = 1
	revId := revisionID{namespace, service}
	kubeClient := fakekubeclientset.NewSimpleClientset()

	observer := initEndpointObserver(kubeClient)
	observer.Watch(revId)

	createAndUpdateEndpoints(0, 1, kubeClient, service)

	assertEndpoints(want, revId, observer, t)
}

// Make sure the handler is working for the case of scale down
func TestEndpointsObserver_Watch_Scale_Down(t *testing.T) {
	subsets = 1
	want = 0
	revId := revisionID{namespace, service}
	kubeClient := fakekubeclientset.NewSimpleClientset()

	observer := initEndpointObserver(kubeClient)
	observer.Watch(revId)

	createAndUpdateEndpoints(1, 0, kubeClient, service)

	assertEndpoints(want, revId, observer, t)
}

// Make sure several endpoints could be saved
func TestEndpointsObserver_Watch_Several_Endpoints(t *testing.T) {
	subsets = 1
	want = 1
	secondServiceName := service+"1"
	firstRevision := revisionID{namespace, service}
	secondRevision := revisionID{namespace, secondServiceName}
	kubeClient := fakekubeclientset.NewSimpleClientset()

	observer := initEndpointObserver(kubeClient)
	observer.Watch(firstRevision)
	observer.Watch(secondRevision)

	createAndUpdateEndpoints(0, 1, kubeClient, service)
	createAndUpdateEndpoints(0, 1, kubeClient, secondServiceName)

	assertEndpoints(want, firstRevision, observer, t)
	assertEndpoints(want, secondRevision, observer, t)
}