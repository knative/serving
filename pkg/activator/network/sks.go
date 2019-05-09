package network

import (
	"errors"
	"fmt"

	"github.com/knative/serving/pkg/apis/networking"
	netlisters "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	"github.com/knative/serving/pkg/network"

	corev1listers "k8s.io/client-go/listers/core/v1"
)

// PrivateEndpointForRevision returns the name of the private endpoint resource for a given revision
func PrivateEndpointForRevision(namespace, name string, protocol networking.ProtocolType, sksLister netlisters.ServerlessServiceLister, serviceLister corev1listers.ServiceLister) (string, error) {
	// SKS name matches that of revision.
	sks, err := sksLister.ServerlessServices(namespace).Get(name)
	if err != nil {
		return "", err
	}
	return serviceEndpoint(namespace, sks.Status.PrivateServiceName, protocol, serviceLister)
}

// serviceHostName obtains the hostname of the underlying service and the correct
// port to send requests to.
func serviceEndpoint(namespace, serviceName string, protocol networking.ProtocolType, serviceLister corev1listers.ServiceLister) (string, error) {
	svc, err := serviceLister.Services(namespace).Get(serviceName)
	if err != nil {
		return "", err
	}

	// Search for the appropriate port
	port := -1
	for _, p := range svc.Spec.Ports {
		if p.Name == networking.ServicePortName(protocol) {
			port = int(p.Port)
			break
		}
	}
	if port == -1 {
		return "", errors.New("revision needs external HTTP port")
	}

	serviceFQDN := network.GetServiceHostname(serviceName, namespace)

	return fmt.Sprintf("%s:%d", serviceFQDN, port), nil
}
