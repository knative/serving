package main

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/kelseyhightower/envconfig"

	"google.golang.org/api/dns/v1"

	"k8s.io/apimachinery/pkg/util/wait"

	"knative.dev/serving/test/e2e/autotls/config"
)

var env config.EnvConfig

func main() {
	if err := envconfig.Process("", &env); err != nil {
		log.Fatalf("Failed to process environment variable: %v.", err)
	}
	if err := setupDNSRecord(); err != nil {
		log.Fatalf("Failed to setup DNS record: %v", err)
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
	if err := waitForDNSRecordVisibleLocally(dnsRecord); err != nil {
		config.DeleteDNSRecord(dnsRecord, env.CloudDNSServiceAccountKeyFile, env.CloudDNSProject, env.DNSZone)
		return err
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

func waitForDNSRecordVisibleLocally(record *config.DNSRecord) error {
	return wait.PollImmediate(10*time.Second, 300*time.Second, func() (bool, error) {
		ips, _ := net.LookupHost(record.Domain)
		for _, ip := range ips {
			if ip == record.IP {
				return true, nil
			}
		}
		return false, nil
	})
}
