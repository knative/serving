/*
Copyright 2019 The Knative Authors

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
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/knative/test-infra/shared/junit"
	"github.com/knative/test-infra/shared/mysql"
)

const (
	presubmit  = "presubmit"
	dbName     = "knative_performance"
	dbInstance = "knative-tests:us-central1:knative-monitoring"
	insertStmt = `
	INSERT INTO METRICS (
		RunId, JobType, TestName, MetricName, MetricValue
		) VALUES (?, ?, ?, ?, ?)`

	// Path to secrets for username and password
	userSecret = "/secrets/cloudsql/monitoringdb/username"
	passSecret = "/secrets/cloudsql/monitoringdb/password"

	// Property name used by testgrid
	perfLatency = "perf_latency"
)

// CreatePerfTestCase creates a perf test case with the provided name and value
func CreatePerfTestCase(metricValue float32, metricName, testName string) junit.TestCase {
	tp := []junit.TestProperty{{Name: perfLatency, Value: fmt.Sprintf("%f", metricValue)}}
	tc := junit.TestCase{
		ClassName:  testName,
		Name:       fmt.Sprintf("%s/%s", testName, metricName),
		Properties: junit.TestProperties{Properties: tp}}

	db, err := ConfigureDB()
	if err == nil {
		if err = db.StoreMetrics(testName, metricName, metricValue); err != nil {
			log.Printf("Cannot store metrics %s for %s due to: %v", metricName, testName, err)
		}
	} else {
		log.Printf("Cannot configure db: %v", err)
	}
	return tc
}

type DBConfig struct {
	*mysql.DBConfig
}

// Configure the db instance to store metrics information.
// This will be later used to show the trending metrics on our grafana dashboard.
func ConfigureDB() (*DBConfig, error) {
	config, err := mysql.ConfigureDB(userSecret, passSecret, dbName, dbInstance)
	return &DBConfig{config}, err
}

func (c *DBConfig) StoreMetrics(tName, metricName string, metricValue float32) error {
	// Get values of env vars set up by Prow. Ignore storage for local runs
	runId := os.Getenv("BUILD_ID")
	jobType := os.Getenv("JOB_TYPE")
	if len(runId) == 0 || len(jobType) == 0 {
		log.Printf("Build id or job type not set. Not storing metric %s", metricName)
		return nil
	}

	if strings.ToLower(jobType) == presubmit {
		log.Printf("Ignoring metric %s storage as this is a pre-submit job", metricName)
		return nil
	}

	// Store the metrics if the perf tests are run by prow(ci/cd) and if its periodic runs
	db, err := c.Connect()
	if err != nil {
		return err
	}
	defer db.Close()

	// Execute the insert statement to add metric information
	res, err := db.Exec(insertStmt, runId, jobType, tName, metricName, metricValue)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("expected to affect 1 row, affected %d", rows)
	}

	return nil
}
