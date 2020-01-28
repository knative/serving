// +build e2e

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
package http01

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"testing"
	"time"

	"github.com/kelseyhightower/envconfig"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/dns/v1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"knative.dev/pkg/test/ingress"
	routenames "knative.dev/serving/pkg/reconciler/route/resources/names"
	"knative.dev/serving/test"
	testingress "knative.dev/serving/test/conformance/ingress"
	"knative.dev/serving/test/e2e"
	autotls "knative.dev/serving/test/e2e/autotls"
	v1test "knative.dev/serving/test/v1"
)

const (
	dnsRecordDeadline = 600 // 10 min
)

type dnsRecord struct {
	ip     string
	domain string
}

type config struct {
	DnsZone                       string `split_words:"true" required:"false"`
	CloudDnsServiceAccountKeyFile string `split_words:"true" required:"false"`
	CloudDnsProject               string `split_words:"true" required:"false"`
	SetUpDns                      string `split_words:"true" required:"false"`
}

// To run this test locally, you need:
// 1. Configure config-domain ConfigMap under knative-serivng namespace to use your custom domain.
// 2. In your DNS server, map your custom domain (e.g. *.example.com) to the IP of ingress, and
// make sure it is effective.
// 3. Run the command below to do the configuration:
// kubectl apply -f test/config/autotls/certmanager/http01/
func TestPerKsvcCert_HTTP01(t *testing.T) {
	clients := e2e.Setup(t)
	autotls.DisableNamespaceCert(t, clients)

	// Set up Test environment variable.
	var env config
	if err := envconfig.Process("", &env); err != nil {
		t.Fatalf("Failed to process environment variable: %v.", err)
	}

	names := test.ResourceNames{
		Service: fmt.Sprintf("t%d", time.Now().Unix()),
		Image:   "runtime",
	}
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	objects, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}
	if env.SetUpDns == "true" {
		cancel := setupDNSRecord(t, env, clients, objects.Route.Status.URL.Host)
		defer cancel()
	}

	cancel := autotls.TurnOnAutoTLS(t, clients)
	defer cancel()
	test.CleanupOnInterrupt(cancel)

	// wait for http01 challenge path to be populated.
	waitForHTTP01ChallengePath(t, clients, objects)

	// wait for certificate to be ready
	autotls.WaitForCertificateReady(t, clients, routenames.Certificate(objects.Route))

	// curl HTTPS
	rootCAs := autotls.CreateRootCAs(t, clients, objects.Route.Namespace, routenames.Certificate(objects.Route))
	httpsClient := autotls.CreateHTTPSClient(t, clients, objects, rootCAs)
	testingress.RuntimeRequest(t, httpsClient, "https://"+objects.Service.Status.URL.Host)
}

func waitForHTTP01ChallengePath(t *testing.T, clients *test.Clients, objects *v1test.ResourceObjects) {
	certName := routenames.Certificate(objects.Route)
	if err := wait.Poll(5*time.Second, 60*time.Second, func() (bool, error) {
		cert, err := clients.NetworkingClient.Certificates.Get(certName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Certificate %s has not been created: %v", certName, err)
				return false, nil
			}
			return false, err
		}
		if len(cert.Status.HTTP01Challenges) == 0 {
			return false, nil
		}
		ingress, err := clients.NetworkingClient.Ingresses.Get(objects.Route.Name, metav1.GetOptions{})
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

func setupDNSRecord(t *testing.T, cfg config, clients *test.Clients, domain string) context.CancelFunc {
	ip, err := ingress.GetIngressEndpoint(clients.KubeClient.Kube)
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
		ips, _ := net.LookupHost(record.domain)
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
	return changeDNSRecord(cfg, addition, svc)
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
		// Setting TTL of DNS record to 5 second to make DNS become effective more quickly.
		Ttl:  int64(5),
		Type: "A",
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
