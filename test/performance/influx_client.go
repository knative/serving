/*
Copyright 2022 The Knative Authors

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
	"os"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

const (
	influxToken             = "INFLUX_TOKEN"
	influxURL               = "INFLUX_URL"
	prowTag                 = "PROW_TAG"
	org                     = "Knativetest"
	bucket                  = "knative-serving"
	influxURLSecretVolume   = "influx-url-secret-volume"
	influxTokenSecretVolume = "influx-token-secret-volume"
	influxURLSecretKey      = "influxdb-url"
	influxTokenSecretKey    = "influxdb-token"
)

func AddInfluxPoint(measurement string, fields map[string]interface{}) error {

	url, err := getSecretValue(influxURLSecretVolume, influxURLSecretKey, influxURL)
	if err != nil {
		return err
	}

	token, err := getSecretValue(influxTokenSecretVolume, influxTokenSecretKey, influxToken)
	if err != nil {
		return err
	}

	tags := map[string]string{}
	build, found := os.LookupEnv(prowTag)
	if found {
		tags[prowTag] = build
	}

	client := influxdb2.NewClientWithOptions(url, token,
		influxdb2.DefaultOptions().
			SetUseGZip(true).
			SetBatchSize(20).
			//nolint:gosec // We explicitly don't need to check certs here since this is test code.
			SetTLSConfig(&tls.Config{InsecureSkipVerify: true}))
	defer client.Close()

	writeAPI := client.WriteAPI(org, bucket)
	p := influxdb2.NewPoint(measurement,
		tags,
		fields,
		time.Now())
	// Write point asynchronously
	writeAPI.WritePoint(p)
	// Force all unwritten data to be sent
	writeAPI.Flush()

	return nil
}

func getSecretValue(secretVolume, secretKey, envVarName string) (string, error) {
	value, err := os.ReadFile(fmt.Sprintf("/etc/%s/%s", secretVolume, secretKey))
	if err != nil {
		valueFromEnv, ok := os.LookupEnv(envVarName)
		if !ok {
			return "", fmt.Errorf("failed to get INFLUX %s", secretKey)
		}
		return valueFromEnv, nil
	}
	return string(value), nil
}
