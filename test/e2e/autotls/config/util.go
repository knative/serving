package config

import (
	"io/ioutil"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/dns/v1"
)

type EnvConfig struct {
	FullHostName                  string `envconfig:"full_host_name" required: "true"`
	DNSZone                       string `envconfig:"dns_zone" required:"true"`
	CloudDNSServiceAccountKeyFile string `envconfig:"cloud_dns_service_account_key_file" required:"true"`
	CloudDNSProject               string `envconfig:"cloud_dns_project" required:"true"`
	IngressIP                     string `envconfig:"ingress_ip" required:"true"`
}

type DNSRecord struct {
	IP     string
	Domain string
}

func MakeRecordSet(record *DNSRecord) *dns.ResourceRecordSet {
	dnsName := record.Domain + "."
	return &dns.ResourceRecordSet{
		Name:    dnsName,
		Rrdatas: []string{record.IP},
		// Setting TTL of DNS record to 5 second to make DNS become effective more quickly.
		Ttl:  int64(5),
		Type: "A",
	}
}

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

func ChangeDNSRecord(change *dns.Change, svc *dns.Service, dnsProject, dnsZone string) error {
	chg, err := svc.Changes.Create(dnsProject, dnsZone, change).Do()
	if err != nil {
		return err
	}
	// wait for change to be acknowledged
	for chg.Status == "pending" {
		time.Sleep(time.Second)
		chg, err = svc.Changes.Get(dnsProject, dnsZone, chg.Id).Do()
		if err != nil {
			return err
		}
	}
	return nil
}

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
	client := conf.Client(oauth2.NoContext)
	svc, err := dns.New(client)
	if err != nil {
		return nil, err
	}
	return svc, nil
}
