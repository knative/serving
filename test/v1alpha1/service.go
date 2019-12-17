/*
Copyright 2019 The Knative Authors

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

// service.go provides methods to perform actions on the service resource.

package v1alpha1

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/watch"
	"knative.dev/pkg/test/spoof"

	"github.com/mattbaird/jsonpatch"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/pkg/test/logging"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	serviceresourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/networking"
	rtesting "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
)

const (
	// Namespace is the namespace of the ingress gateway
	Namespace = "knative-serving"

	// GatewayName is the name of the ingress gateway
	GatewayName = networking.KnativeIngressGateway
)

func validateCreatedServiceStatus(clients *test.Clients, names *test.ResourceNames) error {
	return CheckServiceState(clients.ServingAlphaClient, names.Service, func(s *v1alpha1.Service) (bool, error) {
		if s.Status.URL == nil || s.Status.URL.Host == "" {
			return false, fmt.Errorf("URL is not present in Service status: %v", s)
		}
		names.URL = s.Status.URL.URL()
		if s.Status.LatestCreatedRevisionName == "" {
			return false, fmt.Errorf("LatestCreatedCreatedRevisionName is not present in Service status: %v", s)
		}
		names.Revision = s.Status.LatestCreatedRevisionName
		if s.Status.LatestReadyRevisionName == "" {
			return false, fmt.Errorf("LatestReadyRevisionName is not present in Service status: %v", s)
		}
		if s.Status.ObservedGeneration != 1 {
			return false, fmt.Errorf("ObservedGeneration is not 1 in Service status: %v", s)
		}
		return true, nil
	})
}

// GetResourceObjects obtains the services resources from the k8s API server.
func GetResourceObjects(clients *test.Clients, names test.ResourceNames) (*ResourceObjects, error) {
	routeObject, err := clients.ServingAlphaClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	serviceObject, err := clients.ServingAlphaClient.Services.Get(names.Service, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	configObject, err := clients.ServingAlphaClient.Configs.Get(names.Config, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	revisionObject, err := clients.ServingAlphaClient.Revisions.Get(names.Revision, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return &ResourceObjects{
		Route:    routeObject,
		Service:  serviceObject,
		Config:   configObject,
		Revision: revisionObject,
	}, nil
}

// CreateRunLatestServiceReady creates a new Service in state 'Ready'. This function expects Service and Image name passed in through 'names'.
// Names is updated with the Route and Configuration created by the Service and ResourceObjects is returned with the Service, Route, and Configuration objects.
// If this function is called with https == true, the gateway MUST be restored afterwards.
// Returns error if the service does not come up correctly.
func CreateRunLatestServiceReady(t pkgTest.TLegacy, clients *test.Clients, names *test.ResourceNames, https bool, fopt ...rtesting.ServiceOption) (*ResourceObjects, *spoof.TransportOption, error) {
	if names.Image == "" {
		return nil, nil, fmt.Errorf("expected non-empty Image name; got Image=%v", names.Image)
	}

	t.Log("Creating a new Service.", "service", names.Service)
	svc, err := CreateLatestService(t, clients, *names, fopt...)
	if err != nil {
		return nil, nil, err
	}

	// Populate Route and Configuration Objects with name
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)

	// If the Service name was not specified, populate it
	if names.Service == "" {
		names.Service = svc.Name
	}

	t.Log("Waiting for Service to transition to Ready.", "service", names.Service)
	if err = WaitForServiceState(clients.ServingAlphaClient, names.Service, IsServiceReady, "ServiceIsReady"); err != nil {
		return nil, nil, err
	}

	t.Log("Checking to ensure Service Status is populated for Ready service")
	err = validateCreatedServiceStatus(clients, names)
	if err != nil {
		return nil, nil, err
	}

	var httpsTransportOption *spoof.TransportOption
	if https {
		tlsOptions := &istiov1alpha3.Server_TLSOptions{
			Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
			PrivateKey:        "/etc/istio/ingressgateway-certs/tls.key",
			ServerCertificate: "/etc/istio/ingressgateway-certs/tls.crt",
		}
		servers := []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-https",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: tlsOptions,
		}}
		httpsTransportOption, err = setupHTTPS(t, clients.KubeClient, names.URL.Host)
		if err != nil {
			return nil, nil, err
		}
		setupGateway(t, clients, servers)
	}

	t.Log("Getting latest objects Created by Service")
	resources, err := GetResourceObjects(clients, *names)
	if err == nil {
		t.Log("Successfully created Service", names.Service)
	}
	return resources, httpsTransportOption, err
}

// CreateRunLatestServiceLegacyReady creates a new Service in state 'Ready'. This function expects Service and Image name passed in through 'names'.
// Names is updated with the Route and Configuration created by the Service and ResourceObjects is returned with the Service, Route, and Configuration objects.
// Returns error if the service does not come up correctly.
func CreateRunLatestServiceLegacyReady(t pkgTest.T, clients *test.Clients, names *test.ResourceNames, fopt ...rtesting.ServiceOption) (*ResourceObjects, error) {
	if names.Image == "" {
		return nil, fmt.Errorf("expected non-empty Image name; got Image=%v", names.Image)
	}

	t.Log("Creating a new Service.", "service", names.Service)
	svc, err := CreateLatestServiceLegacy(t, clients, *names, fopt...)
	if err != nil {
		return nil, err
	}

	// Populate Route and Configuration Objects with name
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)

	// If the Service name was not specified, populate it
	if names.Service == "" {
		names.Service = svc.Name
	}

	t.Log("Waiting for Service to transition to Ready.", "service", names.Service)
	if err := WaitForServiceState(clients.ServingAlphaClient, names.Service, IsServiceReady, "ServiceIsReady"); err != nil {
		return nil, err
	}

	t.Log("Checking to ensure Service Status is populated for Ready service", names.Service)
	err = validateCreatedServiceStatus(clients, names)
	if err != nil {
		return nil, err
	}

	t.Log("Getting latest objects Created by Service", names.Service)
	resources, err := GetResourceObjects(clients, *names)
	if err == nil {
		t.Log("Successfully created Service", names.Service)
	}
	return resources, err
}

// CreateLatestService creates a service in namespace with the name names.Service and names.Image
func CreateLatestService(t pkgTest.T, clients *test.Clients, names test.ResourceNames, fopt ...rtesting.ServiceOption) (*v1alpha1.Service, error) {
	service := LatestService(names, fopt...)
	LogResourceObject(t, ResourceObjects{Service: service})
	svc, err := clients.ServingAlphaClient.Services.Create(service)
	return svc, err
}

// CreateLatestServiceLegacy creates a service in namespace with the name names.Service and names.Image
func CreateLatestServiceLegacy(t pkgTest.T, clients *test.Clients, names test.ResourceNames, fopt ...rtesting.ServiceOption) (*v1alpha1.Service, error) {
	service := LatestServiceLegacy(names, fopt...)
	LogResourceObject(t, ResourceObjects{Service: service})
	svc, err := clients.ServingAlphaClient.Services.Create(service)
	return svc, err
}

// PatchServiceImage patches the existing service passed in with a new imagePath. Returns the latest service object
func PatchServiceImage(t pkgTest.T, clients *test.Clients, svc *v1alpha1.Service, imagePath string) (*v1alpha1.Service, error) {
	newSvc := svc.DeepCopy()
	if svc.Spec.DeprecatedRunLatest != nil {
		newSvc.Spec.DeprecatedRunLatest.Configuration.GetTemplate().Spec.GetContainer().Image = imagePath
	} else if svc.Spec.DeprecatedRelease != nil {
		newSvc.Spec.DeprecatedRelease.Configuration.GetTemplate().Spec.GetContainer().Image = imagePath
	} else if svc.Spec.DeprecatedPinned != nil {
		newSvc.Spec.DeprecatedPinned.Configuration.GetTemplate().Spec.GetContainer().Image = imagePath
	} else {
		newSvc.Spec.ConfigurationSpec.GetTemplate().Spec.GetContainer().Image = imagePath
	}
	LogResourceObject(t, ResourceObjects{Service: newSvc})
	patchBytes, err := test.CreateBytePatch(svc, newSvc)
	if err != nil {
		return nil, err
	}
	return clients.ServingAlphaClient.Services.Patch(svc.ObjectMeta.Name, types.JSONPatchType, patchBytes, "")
}

// PatchService creates and applies a patch from the diff between curSvc and desiredSvc. Returns the latest service object.
func PatchService(t pkgTest.T, clients *test.Clients, curSvc *v1alpha1.Service, desiredSvc *v1alpha1.Service) (*v1alpha1.Service, error) {
	LogResourceObject(t, ResourceObjects{Service: desiredSvc})
	patchBytes, err := test.CreateBytePatch(curSvc, desiredSvc)
	if err != nil {
		return nil, err
	}
	return clients.ServingAlphaClient.Services.Patch(curSvc.ObjectMeta.Name, types.JSONPatchType, patchBytes, "")
}

// UpdateServiceRouteSpec updates a service to use the route name in names.
func UpdateServiceRouteSpec(t pkgTest.T, clients *test.Clients, names test.ResourceNames, rs v1alpha1.RouteSpec) (*v1alpha1.Service, error) {
	patches := []jsonpatch.JsonPatchOperation{{
		Operation: "replace",
		Path:      "/spec/traffic",
		Value:     rs.Traffic,
	}}
	patchBytes, err := json.Marshal(patches)
	if err != nil {
		return nil, err
	}
	return clients.ServingAlphaClient.Services.Patch(names.Service, types.JSONPatchType, patchBytes, "")
}

// PatchServiceTemplateMetadata patches an existing service by adding metadata to the service's RevisionTemplateSpec.
func PatchServiceTemplateMetadata(t pkgTest.T, clients *test.Clients, svc *v1alpha1.Service, metadata metav1.ObjectMeta) (*v1alpha1.Service, error) {
	newSvc := svc.DeepCopy()
	newSvc.Spec.ConfigurationSpec.Template.ObjectMeta = metadata
	LogResourceObject(t, ResourceObjects{Service: newSvc})
	patchBytes, err := test.CreateBytePatch(svc, newSvc)
	if err != nil {
		return nil, err
	}
	return clients.ServingAlphaClient.Services.Patch(svc.ObjectMeta.Name, types.JSONPatchType, patchBytes, "")
}

// WaitForServiceLatestRevision takes a revision in through names and compares it to the current state of LatestCreatedRevisionName in Service.
// Once an update is detected in the LatestCreatedRevisionName, the function waits for the created revision to be set in LatestReadyRevisionName
// before returning the name of the revision.
func WaitForServiceLatestRevision(clients *test.Clients, names test.ResourceNames) (string, error) {
	var revisionName string
	if err := WaitForServiceState(clients.ServingAlphaClient, names.Service, func(s *v1alpha1.Service) (bool, error) {
		if s.Status.LatestCreatedRevisionName != names.Revision {
			revisionName = s.Status.LatestCreatedRevisionName
			// We also check that the revision is pinned, meaning it's not a stale revision.
			// Without this it might happen that the latest created revision is later overridden by a newer one
			// and the following check for LatestReadyRevisionName would fail.
			if revErr := CheckRevisionState(clients.ServingAlphaClient, revisionName, IsRevisionPinned); revErr != nil {
				return false, nil
			}
			return true, nil
		}
		return false, nil
	}, "ServiceUpdatedWithRevision"); err != nil {
		return "", fmt.Errorf("LatestCreatedRevisionName not updated: %w", err)
	}
	if err := WaitForServiceState(clients.ServingAlphaClient, names.Service, func(s *v1alpha1.Service) (bool, error) {
		return (s.Status.LatestReadyRevisionName == revisionName), nil
	}, "ServiceReadyWithRevision"); err != nil {
		return "", fmt.Errorf("LatestReadyRevisionName not updated with %s: %w", revisionName, err)
	}

	return revisionName, nil
}

// LatestService returns a Service object in namespace with the name names.Service
// that uses the image specified by names.Image.
func LatestService(names test.ResourceNames, fopt ...rtesting.ServiceOption) *v1alpha1.Service {
	a := append([]rtesting.ServiceOption{
		rtesting.WithInlineConfigSpec(*ConfigurationSpec(pkgTest.ImagePath(names.Image))),
	}, fopt...)
	return rtesting.ServiceWithoutNamespace(names.Service, a...)
}

// LatestServiceLegacy returns a DeprecatedRunLatest Service object in namespace with the name names.Service
// that uses the image specified by names.Image.
func LatestServiceLegacy(names test.ResourceNames, fopt ...rtesting.ServiceOption) *v1alpha1.Service {
	a := append([]rtesting.ServiceOption{
		rtesting.WithRunLatestConfigSpec(*LegacyConfigurationSpec(pkgTest.ImagePath(names.Image))),
	}, fopt...)
	svc := rtesting.ServiceWithoutNamespace(names.Service, a...)
	// Clear the name, which is put there by defaulting.
	svc.Spec.DeprecatedRunLatest.Configuration.GetTemplate().Spec.GetContainer().Name = ""
	return svc
}

// WaitForServiceState polls the status of the Service called name
// from client every `PollInterval` until `inState` returns `true` indicating it
// is done, returns an error or PollTimeout. desc will be used to name the metric
// that is emitted to track how long it took for name to get into the state checked by inState.
func WaitForServiceState(client *test.ServingAlphaClients, name string, inState func(s *v1alpha1.Service) (bool, error), desc string) error {
	span := logging.GetEmitableSpan(context.Background(), fmt.Sprintf("WaitForServiceState/%s/%s", name, desc))
	defer span.End()

	var lastState *v1alpha1.Service
	waitErr := wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		var err error
		lastState, err = client.Services.Get(name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		return inState(lastState)
	})

	if waitErr != nil {
		return fmt.Errorf("service %q is not in desired state, got: %+v: %w", name, lastState, waitErr)
	}
	return nil
}

// CheckServiceState verifies the status of the Service called name from client
// is in a particular state by calling `inState` and expecting `true`.
// This is the non-polling variety of WaitForServiceState.
func CheckServiceState(client *test.ServingAlphaClients, name string, inState func(s *v1alpha1.Service) (bool, error)) error {
	s, err := client.Services.Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if done, err := inState(s); err != nil {
		return err
	} else if !done {
		return fmt.Errorf("service %q is not in desired state, got: %+v", name, s)
	}
	return nil
}

// IsServiceReady will check the status conditions of the service and return true if the service is
// ready. This means that its configurations and routes have all reported ready.
func IsServiceReady(s *v1alpha1.Service) (bool, error) {
	return s.Generation == s.Status.ObservedGeneration && s.Status.IsReady(), nil
}

// IsServiceNotReady checks the Ready status condition of the service and returns true only if Ready is set to False.
func IsServiceNotReady(s *v1alpha1.Service) (bool, error) {
	result := s.Status.GetCondition(v1alpha1.ServiceConditionReady)
	return s.Generation == s.Status.ObservedGeneration && result != nil && result.Status == corev1.ConditionFalse, nil
}

// IsServiceRoutesNotReady checks the RoutesReady status of the service and returns true only if RoutesReady is set to False.
func IsServiceRoutesNotReady(s *v1alpha1.Service) (bool, error) {
	result := s.Status.GetCondition(v1alpha1.ServiceConditionRoutesReady)
	return s.Generation == s.Status.ObservedGeneration && result != nil && result.Status == corev1.ConditionFalse, nil
}

// RestoreGateway updates the gateway object to the oldGateway
func RestoreGateway(t pkgTest.TLegacy, clients *test.Clients, oldGateway v1alpha3.Gateway) {
	currGateway, err := clients.IstioClient.NetworkingV1alpha3().Gateways(Namespace).Get(GatewayName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(fmt.Sprintf("Failed to get Gateway %s/%s", Namespace, GatewayName))
	}
	if equality.Semantic.DeepEqual(*currGateway, oldGateway) {
		t.Log("Gateway not restored because it's still the same")
		return
	}
	currGateway.Spec.Servers = oldGateway.Spec.Servers
	if _, err := clients.IstioClient.NetworkingV1alpha3().Gateways(Namespace).Update(currGateway); err != nil {
		t.Fatal(fmt.Sprintf("Failed to restore Gateway %s/%s: %v", Namespace, GatewayName, err))
	}
}

// setupGateway updates the ingress Gateway to the provided Servers and waits until all Envoy pods have been updated.
func setupGateway(t pkgTest.TLegacy, clients *test.Clients, servers []*istiov1alpha3.Server) {
	// Get the current Gateway
	curGateway, err := clients.IstioClient.NetworkingV1alpha3().Gateways(Namespace).Get(GatewayName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(fmt.Sprintf("Failed to get Gateway %s/%s: %v", Namespace, GatewayName, err))
	}

	// Update its Spec
	newGateway := curGateway.DeepCopy()
	newGateway.Spec.Servers = servers

	// Update the Gateway
	gw, err := clients.IstioClient.NetworkingV1alpha3().Gateways(Namespace).Update(newGateway)
	if err != nil {
		t.Fatal(fmt.Sprintf("Failed to update Gateway %s/%s: %v", Namespace, GatewayName, err))
	}

	var selectors []string
	for k, v := range gw.Spec.Selector {
		selectors = append(selectors, k+"="+v)
	}
	selector := strings.Join(selectors, ",")

	// Restart the Gateway pods: this is needed because Istio without SDS won't refresh the cert when the secret is updated
	pods, err := clients.KubeClient.Kube.CoreV1().Pods("istio-system").List(metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		t.Fatal("Failed to list Gateway pods", "error", err.Error())
	}

	// TODO(bancel): there is a race condition here if a pod listed in the call above is deleted before calling watch below

	var wg sync.WaitGroup
	wg.Add(len(pods.Items))
	wtch, err := clients.KubeClient.Kube.CoreV1().Pods("istio-system").Watch(metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		t.Fatal("Failed to watch Gateway pods", "error", err.Error())
	}
	defer wtch.Stop()

	done := make(chan struct{})
	go func() {
		for {
			select {
			case event := <-wtch.ResultChan():
				if event.Type == watch.Deleted {
					wg.Done()
				}
			case <-done:
				return
			}
		}
	}()

	err = clients.KubeClient.Kube.CoreV1().Pods("istio-system").DeleteCollection(&metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		t.Fatal("Failed to delete Gateway pods", "error", err.Error())
	}

	wg.Wait()
	done <- struct{}{}
}

// setupHTTPS creates a self-signed certificate, installs it as a Secret and returns an *http.Transport
// trusting the certificate as a root CA.
func setupHTTPS(t pkgTest.T, kubeClient *pkgTest.KubeClient, host string) (*spoof.TransportOption, error) {
	t.Helper()
	cert, key, err := generateCertificate(host)
	if err != nil {
		return nil, err
	}

	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	if ok := rootCAs.AppendCertsFromPEM(cert); !ok {
		return nil, errors.New("failed to add the certificate to the root CA")
	}

	kubeClient.Kube.CoreV1().Secrets("istio-system").Delete("istio-ingressgateway-certs", &metav1.DeleteOptions{})
	_, err = kubeClient.Kube.CoreV1().Secrets("istio-system").Create(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "istio-system",
			Name:      "istio-ingressgateway-certs",
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.key": key,
			"tls.crt": cert,
		},
	})
	if err != nil {
		return nil, err
	}
	var transportOption spoof.TransportOption = func(transport *http.Transport) *http.Transport {
		transport.TLSClientConfig = &tls.Config{RootCAs: rootCAs}
		return transport
	}
	return &transportOption, nil
}

// generateCertificate generates a self-signed certificate for the provided host and returns
// the PEM encoded certificate and private key.
func generateCertificate(host string) ([]byte, []byte, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %v", err)
	}

	notBefore := time.Now().Add(-5 * time.Minute)
	notAfter := notBefore.Add(2 * time.Hour)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate serial number: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Knative Serving"},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = append(template.IPAddresses, ip)
	} else {
		template.DNSNames = append(template.DNSNames, host)
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create the certificate: %v", err)
	}

	var certBuf bytes.Buffer
	if err := pem.Encode(&certBuf, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return nil, nil, fmt.Errorf("failed to encode the certificate: %v", err)
	}

	var keyBuf bytes.Buffer
	if err := pem.Encode(&keyBuf, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}); err != nil {
		return nil, nil, fmt.Errorf("failed to encode the private key: %v", err)
	}

	return certBuf.Bytes(), keyBuf.Bytes(), nil
}
