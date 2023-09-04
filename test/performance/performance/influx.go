/*
Copyright 2023 The Knative Authors

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

package performance

import (
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
)

const (
	influxToken = "INFLUX_TOKEN"
	influxURL   = "INFLUX_URL"
	prowTag     = "PROW_TAG"
	org         = "Knativetest"
	bucket      = "knative-serving"
)

// InfluxReporter wraps a influxdb client
type InfluxReporter struct {
	client   influxdb2.Client
	writeAPI api.WriteAPI
	tags     map[string]string
}

// NewInfluxReporter creates a InfluxReporter
// The method expects tags to be provided as a map. These are used to identify different runs.
func NewInfluxReporter(tags map[string]string) (*InfluxReporter, error) {
	url, err := getEnvVariable(influxURL)
	if err != nil {
		return nil, err
	}

	token, err := getEnvVariable(influxToken)
	if err != nil {
		return nil, err
	}

	client := influxdb2.NewClientWithOptions(url, token,
		influxdb2.DefaultOptions().
			SetUseGZip(true).
			//nolint:gosec // We explicitly don't need to check certs here since this is test code.
			SetTLSConfig(&tls.Config{InsecureSkipVerify: true}))

	writeAPI := client.WriteAPI(org, bucket)

	build, found := os.LookupEnv(prowTag)
	if found {
		tags[prowTag] = build
	}

	return &InfluxReporter{
		client:   client,
		writeAPI: writeAPI,
		tags:     tags,
	}, nil
}

// FlushAndShutdown flushes the data to influxdb and terminates the client.
func (ir *InfluxReporter) FlushAndShutdown() {
	log.Println("Shutting down InfluxReporter")
	ir.writeAPI.Flush()
	ir.client.Close()
}

// AddDataPoint asynchronously writes a new data-point to influxdb.
func (ir *InfluxReporter) AddDataPoint(measurement string, fields map[string]interface{}) {
	p := influxdb2.NewPoint(measurement,
		ir.tags,
		fields,
		time.Now())

	// Write point asynchronously
	ir.writeAPI.WritePoint(p)
}

func getEnvVariable(envVarName string) (string, error) {
	valueFromEnv, ok := os.LookupEnv(envVarName)
	if !ok {
		return "", fmt.Errorf("failed to get env variable: %s", envVarName)
	}
	return valueFromEnv, nil
}
