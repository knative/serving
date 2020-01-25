/*
Copyright 2020 The Knative Authors

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
package autotls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	pkgtest "knative.dev/pkg/test"
	"knative.dev/pkg/test/ingress"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	cmclientset "knative.dev/serving/pkg/client/certmanager/clientset/versioned"
	routenames "knative.dev/serving/pkg/reconciler/route/resources/names"
	"knative.dev/serving/test"
	testingress "knative.dev/serving/test/conformance/ingress"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"

	"github.com/ghodss/yaml"
	cmv1alpha2 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha2"
	cmmeta "github.com/jetstack/cert-manager/pkg/apis/meta/v1"
	"github.com/kelseyhightower/envconfig"

	"google.golang.org/api/dns/v1"
)

const (
	systemNamespace   = "knative-serving"
	dnsRecordDeadline = 600 // 10 min
)

var (
	caClusterIssuer = &cmv1alpha2.ClusterIssuer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ca-issuer",
		},
		Spec: cmv1alpha2.IssuerSpec{
			IssuerConfig: cmv1alpha2.IssuerConfig{
				CA: &cmv1alpha2.CAIssuer{},
			},
		},
	}
)

type dnsRecord struct {
	ip     string
	domain string
}

type config struct {
	DnsZone                       string `split_words:"true" required:"false"`
	CloudDnsServiceAccountKeyFile string `split_words:"true" required:"false"`
	CloudDnsProject               string `split_words:"true" required:"false"`
	setUpDNS                      string `split_words:"true" required:"false"`
}

type autoTLSClients struct {
	clients  *test.Clients
	cmClient cmclientset.Interface
}

func TestPerKsvcCert_localCA(t *testing.T) {
	tlsClients := initializeClients(t)
	disableNamespaceCert(t, tlsClients)

	// Create Knative Service
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "runtime",
	}
	test.CleanupOnInterrupt(func() { test.TearDown(tlsClients.clients, names) })
	objects, err := v1test.CreateServiceReady(t, tlsClients.clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	// Create TLS certificate for the Knative Service.
	rootCAs := x509.NewCertPool()
	secretName, cancel := testingress.CreateTLSSecretWithCertPool(t, tlsClients.clients, []string{objects.Service.Status.URL.Host}, "cert-manager", rootCAs)
	defer cancel()

	// Create ClusterIssuer and update config-certmanager to reference the created ClusterIssuer
	clusterIssuer, cancel := createClusterIssuer(t, tlsClients, secretName)
	defer cancel()
	cancel = updateConfigCertManangerCM(t, tlsClients, clusterIssuer)
	defer cancel()

	cancel = turnOnAutoTLS(t, tlsClients)
	defer cancel()

	// wait for certificate to be ready
	waitForCertificateReady(t, tlsClients, routenames.Certificate(objects.Route))

	// curl HTTPS
	httpsClient := createHTTPSClient(t, tlsClients, objects, rootCAs)
	testingress.RuntimeRequest(t, httpsClient, "https://"+objects.Service.Status.URL.Host)
}

// To run this test locally, you need:
// 1. Configure config-domain ConfigMap under knative-serivng namespace to use your custom domain.
// 2. In your DNS server, map your custom domain (e.g. *.example.com) to the IP of ingress, and
// make sure it is effective.
func TestPerKsvcCert_HTTP01(t *testing.T) {
	t.Skip("Test environment is not ready. Can only be ran locally.")
	tlsClients := initializeClients(t)
	disableNamespaceCert(t, tlsClients)

	// Set up Test environment variable.
	var env config
	if err := envconfig.Process("", &env); err != nil {
		t.Fatalf("Failed to process environment variable: %v.", err)
	}
	t.Logf("Environment variables are %v.", env)

	// Create Knative Service
	names := test.ResourceNames{
		Service: fmt.Sprintf("t%d", time.Now().Unix()),
		Image:   "runtime",
	}
	test.CleanupOnInterrupt(func() { test.TearDown(tlsClients.clients, names) })
	objects, err := v1test.CreateServiceReady(t, tlsClients.clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}
	if env.setUpDNS == "true" {
		cancel := setupDNSRecord(t, env, tlsClients, objects.Route.Status.URL.Host)
		defer cancel()
	}

	cancel := turnOnAutoTLS(t, tlsClients)
	defer cancel()

	// wait for http01 challenge path to be populated.
	waitForHTTP01ChallengePath(t, tlsClients, objects)

	// wait for certificate to be ready
	waitForCertificateReady(t, tlsClients, routenames.Certificate(objects.Route))

	// curl HTTPS
	rootCAs := createRootCAs(t, tlsClients, objects)
	httpsClient := createHTTPSClient(t, tlsClients, objects, rootCAs)
	testingress.RuntimeRequest(t, httpsClient, "https://"+objects.Service.Status.URL.Host)
}

func createRootCAs(t *testing.T, tlsClients *autoTLSClients, objects *v1test.ResourceObjects) *x509.CertPool {
	secret, err := tlsClients.clients.KubeClient.Kube.CoreV1().Secrets(objects.Route.Namespace).Get(
		routenames.Certificate(objects.Route), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Secret %s: %v", routenames.Certificate(objects.Route), err)
	}

	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	if ok := rootCAs.AppendCertsFromPEM(secret.Data[corev1.TLSCertKey]); !ok {
		t.Fatalf("Failed to add the certificate to the root CA")
	}
	return rootCAs
}

func createHTTPSClient(t *testing.T, tlsClients *autoTLSClients, objects *v1test.ResourceObjects, rootCAs *x509.CertPool) *http.Client {
	ing, err := tlsClients.clients.NetworkingClient.Ingresses.Get(routenames.Ingress(objects.Route), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Ingress %s: %v", routenames.Ingress(objects.Route), err)
	}
	dialer := testingress.CreateDialContext(t, ing, tlsClients.clients)
	tlsConfig := &tls.Config{
		RootCAs: rootCAs,
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:     dialer,
			TLSClientConfig: tlsConfig,
		}}
}

func initializeClients(t *testing.T) *autoTLSClients {
	clientConfig, err := test.BuildClientConfig(pkgtest.Flags.Kubeconfig, pkgtest.Flags.Cluster)
	if err != nil {
		t.Fatalf("Failed to create client config: %v.", err)
	}
	clients := &autoTLSClients{}
	clients.clients = e2e.Setup(t)
	clients.cmClient, err = cmclientset.NewForConfig(clientConfig)
	if err != nil {
		t.Fatalf("Failed to create cert manager client: %v", err)
	}
	return clients
}

func disableNamespaceCert(t *testing.T, tlsClients *autoTLSClients) {
	namespaces, err := tlsClients.clients.KubeClient.Kube.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list namespaces: %v", err)
	}
	for _, ns := range namespaces.Items {
		if ns.Labels == nil {
			ns.Labels = map[string]string{}
		}
		ns.Labels[networking.DisableWildcardCertLabelKey] = "true"
		if _, err := tlsClients.clients.KubeClient.Kube.CoreV1().Namespaces().Update(&ns); err != nil {
			t.Errorf("Fail to disable namespace cert: %v", err)
		}
	}
}

func createClusterIssuer(t *testing.T, tlsClients *autoTLSClients, tlsSecretName string) (*cmv1alpha2.ClusterIssuer, context.CancelFunc) {
	copy := caClusterIssuer.DeepCopy()
	copy.Spec.CA.SecretName = tlsSecretName
	test.CleanupOnInterrupt(func() {
		tlsClients.cmClient.CertmanagerV1alpha2().ClusterIssuers().Delete(copy.Name, &metav1.DeleteOptions{})
	})
	if _, err := tlsClients.cmClient.CertmanagerV1alpha2().ClusterIssuers().Create(copy); err != nil {
		t.Fatalf("Failed to create ClusterIssuer %v: %v", &copy, err)
	}
	return copy, func() {
		if err := tlsClients.cmClient.CertmanagerV1alpha2().ClusterIssuers().Delete(copy.Name, &metav1.DeleteOptions{}); err != nil {
			t.Errorf("Failed to clean up ClusterIssuer %s: %v", copy.Name, err)
		}
	}
}

func updateConfigCertManangerCM(t *testing.T, tlsClients *autoTLSClients, clusterIssuer *cmv1alpha2.ClusterIssuer) context.CancelFunc {
	issuerRef := &cmmeta.ObjectReference{
		Name: clusterIssuer.Name,
		Kind: "ClusterIssuer",
	}
	issuerRefBytes, err := yaml.Marshal(issuerRef)
	if err != nil {
		t.Fatalf("Failed to convert IssuerRef %v to bytes: %v", issuerRef, err)
	}

	certManagerCM, err := tlsClients.clients.KubeClient.Kube.CoreV1().ConfigMaps(systemNamespace).Get("config-certmanager", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get config-certmanager ConfigMap: %v", err)
	}
	certManagerCM.Data["issuerRef"] = string(issuerRefBytes)
	test.CleanupOnInterrupt(func() {
		cleanUpConfigCertManagerCM(t, tlsClients)
	})
	if _, err := tlsClients.clients.KubeClient.Kube.CoreV1().ConfigMaps(certManagerCM.Namespace).Update(certManagerCM); err != nil {
		t.Fatalf("Failed to update the config-certmanager ConfigMap: %v", err)
	}
	return func() {
		cleanUpConfigCertManagerCM(t, tlsClients)
	}
}

func cleanUpConfigCertManagerCM(t *testing.T, tlsClients *autoTLSClients) {
	certManagerCM, err := tlsClients.clients.KubeClient.Kube.CoreV1().ConfigMaps(systemNamespace).Get("config-certmanager", metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to get config-certmanager ConfigMap: %v", err)
		return
	}
	delete(certManagerCM.Data, "issuerRef")
	if _, err := tlsClients.clients.KubeClient.Kube.CoreV1().ConfigMaps(certManagerCM.Namespace).Update(certManagerCM); err != nil {
		t.Errorf("Failed to clean up config-certmanager ConfigMap: %v", err)
	}
}

func turnOnAutoTLS(t *testing.T, tlsClients *autoTLSClients) context.CancelFunc {
	configNetworkCM, err := tlsClients.clients.KubeClient.Kube.CoreV1().ConfigMaps(systemNamespace).Get("config-network", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get config-network ConfigMap: %v", err)
	}
	configNetworkCM.Data["autoTLS"] = "Enabled"
	test.CleanupOnInterrupt(func() {
		turnOffAutoTLS(t, tlsClients)
	})
	if _, err := tlsClients.clients.KubeClient.Kube.CoreV1().ConfigMaps(systemNamespace).Update(configNetworkCM); err != nil {
		t.Fatalf("Failed to update config-network ConfigMap: %v", err)
	}
	return func() {
		turnOffAutoTLS(t, tlsClients)
	}
}

func turnOffAutoTLS(t *testing.T, tlsClients *autoTLSClients) {
	configNetworkCM, err := tlsClients.clients.KubeClient.Kube.CoreV1().ConfigMaps(systemNamespace).Get("config-network", metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to get config-network ConfigMap: %v", err)
		return
	}
	delete(configNetworkCM.Data, "autoTLS")
	if _, err := tlsClients.clients.KubeClient.Kube.CoreV1().ConfigMaps(configNetworkCM.Namespace).Update(configNetworkCM); err != nil {
		t.Errorf("Failed to turn off Auto TLS: %v", err)
	}
}

func waitForHTTP01ChallengePath(t *testing.T, tlsClients *autoTLSClients, objects *v1test.ResourceObjects) {
	certName := routenames.Certificate(objects.Route)
	if err := wait.Poll(5*time.Second, 60*time.Second, func() (bool, error) {
		cert, err := tlsClients.clients.NetworkingClient.Certificates.Get(certName, metav1.GetOptions{})
		cert, ready, err := getCertificate(t, tlsClients, certName)
		if !ready {
			return ready, err
		}
		if len(cert.Status.HTTP01Challenges) == 0 {
			return false, nil
		}
		ingress, err := tlsClients.clients.NetworkingClient.Ingresses.Get(objects.Route.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, rule := range ingress.Spec.Rules {
			for _, path := range rule.HTTP.Paths {
				if path.Path == cert.Status.HTTP01Challenges[0].URL.Path {
					return true, nil
				}
			}
		}
		return false, nil
	}); err != nil {
		t.Fatalf("HTTP01 challenge path is not populated in Ingress: %v", err)
	}
}

func waitForCertificateReady(t *testing.T, tlsClients *autoTLSClients, certName string) {
	if err := wait.Poll(10*time.Second, 300*time.Second, func() (bool, error) {
		cert, ready, err := getCertificate(t, tlsClients, certName)
		if !ready {
			return ready, err
		}
		return cert.Status.IsReady(), nil
	}); err != nil {
		t.Fatalf("Certificate %s is not ready: %v", certName, err)
	}
}

func getCertificate(t *testing.T, tlsClients *autoTLSClients, certName string) (*v1alpha1.Certificate, bool, error) {
	cert, err := tlsClients.clients.NetworkingClient.Certificates.Get(certName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			t.Logf("Certificate %s has not been created: %v", certName, err)
			return nil, false, nil
		}
		return nil, false, err
	}
	return cert, true, nil
}

func setupDNSRecord(t *testing.T, cfg config, tlsClients *autoTLSClients, domain string) context.CancelFunc {
	ip, err := ingress.GetIngressEndpoint(tlsClients.clients.KubeClient.Kube)
	if err != nil {
		t.Fatalf("Failed to get Gateway IP address %v.", err)
	}
	dnsRecord := &dnsRecord{
		domain: domain,
		ip:     ip,
	}
	if err := createDNSRecord(cfg, dnsRecord); err != nil {
		t.Fatalf("Failed to create DNS record: %v", err)
	}
	if err := waitForDNSRecordVisibleLocally(dnsRecord); err != nil {
		deleteDNSRecord(t, cfg, dnsRecord)
		t.Fatalf("Failed to wait for DNS record to be visible: %v", err)
	}
	t.Logf("DNS record %v was set up.", dnsRecord)
	return func() {
		deleteDNSRecord(t, cfg, dnsRecord)
	}
}

func waitForDNSRecordVisibleLocally(record *dnsRecord) error {
	return wait.PollImmediate(10*time.Second, dnsRecordDeadline*time.Second, func() (bool, error) {
		ips, err := net.LookupHost(record.domain)
		if err != nil {
			return false, nil
		}
		for _, ip := range ips {
			if ip == record.ip {
				return true, nil
			}
		}
		return false, nil
	})
}

func createDNSRecord(cfg config, dnsRecord *dnsRecord) error {
	record := makeRecordSet(cfg, dnsRecord)
	svc, err := getCloudDNSSvc(cfg.CloudDnsServiceAccountKeyFile)
	if err != nil {
		return err
	}
	// Look for existing records.
	if list, err := svc.ResourceRecordSets.List(
		cfg.CloudDnsProject, cfg.DnsZone).Name(record.Name).Type("A").Do(); err != nil {
		return err
	} else if len(list.Rrsets) > 0 {
		return fmt.Errorf("record for domain %s already exists", record.Name)
	}

	addition := &dns.Change{
		Additions: []*dns.ResourceRecordSet{record},
	}
	if err := changeDNSRecord(cfg, addition, svc); err != nil {
		return err
	}
	return nil
}

func deleteDNSRecord(t *testing.T, cfg config, dnsRecord *dnsRecord) {
	rec := makeRecordSet(cfg, dnsRecord)
	svc, err := getCloudDNSSvc(cfg.CloudDnsServiceAccountKeyFile)
	if err != nil {
		t.Errorf("Failed to get Cloud DNS service. %v", err)
		return
	}
	deletion := &dns.Change{
		Deletions: []*dns.ResourceRecordSet{rec},
	}
	if err := changeDNSRecord(cfg, deletion, svc); err != nil {
		t.Errorf("Failed to get change DNS record. %v", err)
		return
	}
}

func makeRecordSet(cfg config, record *dnsRecord) *dns.ResourceRecordSet {
	dnsName := record.domain + "."
	return &dns.ResourceRecordSet{
		Name:    dnsName,
		Rrdatas: []string{record.ip},
		Ttl:     int64(10),
		Type:    "A",
	}
}

func changeDNSRecord(cfg config, change *dns.Change, svc *dns.Service) error {
	chg, err := svc.Changes.Create(cfg.CloudDnsProject, cfg.DnsZone, change).Do()
	if err != nil {
		return err
	}
	// wait for change to be acknowledged
	for chg.Status == "pending" {
		time.Sleep(time.Second)
		chg, err = svc.Changes.Get(cfg.CloudDnsProject, cfg.DnsZone, chg.Id).Do()
		if err != nil {
			return err
		}
	}
	return nil
}

// reference: https://github.com/jetstack/cert-manager/blob/master/pkg/issuer/acme/dns/clouddns/clouddns.go
func getCloudDNSSvc(svcAccountKeyFile string) (*dns.Service, error) {
	data, err := ioutil.ReadFile(svcAccountKeyFile)
	if err != nil {
		return nil, err
	}
	conf, err := google.JWTConfigFromJSON(data, dns.NdevClouddnsReadwriteScope)
	if err != nil {
		return nil, err
	}
	client := conf.Client(oauth2.NoContext)
	svc, err := dns.New(client)
	if err != nil {
		return nil, err
	}
	return svc, nil
}
