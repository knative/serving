package dnscleanup

import (
	"log"

	"github.com/kelseyhightower/envconfig"

	"knative.dev/serving/test/e2e/autotls/config"
)

var env config.EnvConfig

func main() {
	if err := envconfig.Process("", &env); err != nil {
		log.Fatalf("Failed to process environment variable: %v.", err)
	}
	record := &config.DNSRecord{
		IP:     env.IngressIP,
		Domain: env.FullHostName,
	}
	if err := config.DeleteDNSRecord(record, env.CloudDNSServiceAccountKeyFile, env.CloudDNSProject, env.DNSZone); err != nil {
		log.Fatalf("Failed to setup DNS record: %v", err)
	}
}
