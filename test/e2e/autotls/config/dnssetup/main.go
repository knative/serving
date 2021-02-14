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

package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"

	"google.golang.org/api/dns/v1"

	"k8s.io/apimachinery/pkg/util/wait"

	"knative.dev/serving/test/e2e/autotls/config"
)

var env config.EnvConfig

func main() {
	if err := envconfig.Process("auto_tls_test", &env); err != nil {
		log.Fatalf("Failed to process environment variable: %v.", err)
	}
	if err := setupDNSRecord(); err != nil {
		log.Fatal("Failed to setup DNS record: ", err)
	}
}

func setupDNSRecord() error {
	dnsRecord := &config.DNSRecord{
		Domain: env.FullHostName,
		IP:     env.IngressIP,
	}
	if err := createDNSRecord(dnsRecord); err != nil {
		return err
	}
	if err := waitForDNSRecordVisible(dnsRecord); err != nil {
		log.Printf("DNS record is not visible yet %v", err)
	}
	return nil
}

func createDNSRecord(dnsRecord *config.DNSRecord) error {
	record := config.MakeRecordSet(dnsRecord)
	svc, err := config.GetCloudDNSSvc(env.CloudDNSServiceAccountKeyFile)
	if err != nil {
		return err
	}
	// Look for existing records.
	if list, err := svc.ResourceRecordSets.List(
		env.CloudDNSProject, env.DNSZone).Name(record.Name).Type("A").Do(); err != nil {
		return err
	} else if len(list.Rrsets) > 0 {
		return fmt.Errorf("record for domain %s already exists", record.Name)
	}

	addition := &dns.Change{
		Additions: []*dns.ResourceRecordSet{record},
	}
	return config.ChangeDNSRecord(addition, svc, env.CloudDNSProject, env.DNSZone)
}

func waitForDNSRecordVisible(record *config.DNSRecord) error {
	nameservers, err := net.LookupNS(env.DomainName)
	if err != nil {
		return err
	}
	var lastErr error
	if err := wait.PollImmediate(10*time.Second, 300*time.Second, func() (bool, error) {
		for _, ns := range nameservers {
			nsIP, err := net.LookupHost(ns.Host)
			if err != nil {
				log.Printf("Failed to look up host %s: %v", ns.Host, err)
				lastErr = err
				return false, nil
			}
			// This resolver bypasses the local resolver and instead queries the
			// domain's authoritative servers.
			r := &net.Resolver{
				Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
					d := net.Dialer{Timeout: 30 * time.Second}
					return d.DialContext(ctx, "udp", nsIP[0]+":53")
				},
			}
			valid, err := validateRecord(r, record)
			if err != nil {
				log.Printf("Failed to validate DNS record %v", err)
				lastErr = err
				return false, nil
			}
			if !valid {
				return false, nil
			}

		}
		return true, nil
	}); err != nil {
		return lastErr
	}
	return nil
}

func validateRecord(resolver *net.Resolver, record *config.DNSRecord) (bool, error) {
	ips, err := resolver.LookupHost(context.Background(), replaceWildcard(record.Domain))
	if err != nil {
		return false, err
	}
	for _, ip := range ips {
		if ip == record.IP {
			return true, nil
		}
	}
	return false, nil
}

func replaceWildcard(domain string) string {
	if domain[0] != '*' {
		return domain
	}

	return strings.Replace(domain, "*", "star", 1)
}
