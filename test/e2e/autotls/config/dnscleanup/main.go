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
		IP:     env.AutoTLSTestIngressIP,
		Domain: env.AutoTLSTestFullHostName,
	}
	if err := config.DeleteDNSRecord(record, env.AutoTLSTestCloudDNSServiceAccountKeyFile, env.AutoTLSTestCloudDNSProject, env.AutoTLSTestDNSZone); err != nil {
		log.Fatal("Failed to setup DNS record: ", err)
	}
}
