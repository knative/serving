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

package config

import (
	"context"
	"io/ioutil"
	"time"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/dns/v1"
	"google.golang.org/api/option"
	"k8s.io/apimachinery/pkg/util/wait"
)

// EnvConfig is the config parsed from environment variables by envconfig.
type EnvConfig struct {
	FullHostName                  string `envconfig:"full_host_name" required:"true"`
	DomainName                    string `envconfig:"domain_name" required:"true"`
	DNSZone                       string `envconfig:"dns_zone" required:"true"`
	CloudDNSServiceAccountKeyFile string `envconfig:"cloud_dns_service_account_key_file" required:"true"`
	CloudDNSProject               string `envconfig:"cloud_dns_project" required:"true"`
	IngressIP                     string `envconfig:"ingress_ip" required:"true"`
}

// DNSRecord represents an IP and Domain.
type DNSRecord struct {
	IP     string
	Domain string
}

// MakeRecordSet creates a dns.ResourceRecordSet for a DNSRecord.
func MakeRecordSet(record *DNSRecord) *dns.ResourceRecordSet {
	dnsName := record.Domain + "."
	return &dns.ResourceRecordSet{
		Name:    dnsName,
		Rrdatas: []string{record.IP},
		// Setting TTL of DNS record to 5 seconds to make DNS become effective more quickly.
		Ttl:  int64(5),
		Type: "A",
	}
}

// DeleteDNSRecord deletes the given DNS record.
func DeleteDNSRecord(record *DNSRecord, svcAccountKeyFile, dnsProject, dnsZone string) error {
	rec := MakeRecordSet(record)
	svc, err := GetCloudDNSSvc(svcAccountKeyFile)
	if err != nil {
		return err
	}
	deletion := &dns.Change{
		Deletions: []*dns.ResourceRecordSet{rec},
	}
	return ChangeDNSRecord(deletion, svc, dnsProject, dnsZone)
}

// ChangeDNSRecord changes the given DNS record.
func ChangeDNSRecord(change *dns.Change, svc *dns.Service, dnsProject, dnsZone string) error {
	chg, err := svc.Changes.Create(dnsProject, dnsZone, change).Do()
	if err != nil {
		return err
	}
	// Wait for change to be acknowledged.
	return wait.PollImmediate(time.Second, 5*time.Minute, func() (bool, error) {
		tmp, err := svc.Changes.Get(dnsProject, dnsZone, chg.Id).Do()
		if err != nil {
			return false, err
		}
		return tmp.Status != "pending", nil
	})
}

// GetCloudDNSSvc returns the Cloud DNS Service stub.
// reference: https://github.com/jetstack/cert-manager/blob/master/pkg/issuer/acme/dns/clouddns/clouddns.go
func GetCloudDNSSvc(svcAccountKeyFile string) (*dns.Service, error) {
	data, err := ioutil.ReadFile(svcAccountKeyFile)
	if err != nil {
		return nil, err
	}
	conf, err := google.JWTConfigFromJSON(data, dns.NdevClouddnsReadwriteScope)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	return dns.NewService(ctx, option.WithHTTPClient(conf.Client(ctx)))
}
