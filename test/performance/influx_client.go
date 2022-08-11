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
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

const (
	influxToken             = "INFLUX_TOKEN"
	influxURL               = "INFLUX_URL"
	prowBuildID             = "BUILD_ID"
	prowPrNumber            = "PULL_NUMBER"
	org                     = "Knativetest"
	bucket                  = "knative-serving"
	influxURLSecretVolume   = "influx-secret-volume"
	influxTokenSecretVolume = "influx-secret-volume"
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
	build, found := os.LookupEnv(prowBuildID)
	if found {
		tags[prowBuildID] = build
	} else {
		tags[prowBuildID] = "local"
	}

	// prow PR number is optional since it doesn't exist for periodic jobs
	pr, found := os.LookupEnv(prowPrNumber)
	if found {
		tags[prowPrNumber] = pr
	}

	client := influxdb2.NewClientWithOptions(url, token,
		influxdb2.DefaultOptions().
			SetUseGZip(true).
			SetTLSConfig(&tls.Config{
				InsecureSkipVerify: true,
			}))
	// User blocking write client for writes to desired bucket
	writeAPI := client.WriteAPIBlocking(org, bucket)
	p := influxdb2.NewPoint(measurement,
		tags,
		fields,
		time.Now())
	// Write point immediately
	err = writeAPI.WritePoint(context.Background(), p)
	if err != nil {
		return err
	}
	// Ensures background processes finishes
	client.Close()
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
