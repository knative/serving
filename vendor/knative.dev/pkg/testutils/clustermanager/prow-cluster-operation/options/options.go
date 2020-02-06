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

package options

import (
	"flag"
	"strings"

	clm "knative.dev/pkg/testutils/clustermanager/e2e-tests"
)

type RequestWrapper struct {
	Request          clm.GKERequest
	BackupRegionsStr string
	AddonsStr        string
	NoWait           bool
}

func NewRequestWrapper() *RequestWrapper {
	rw := &RequestWrapper{
		Request: clm.GKERequest{},
	}
	rw.addOptions()
	return rw
}

func (rw *RequestWrapper) Prep() {
	if rw.BackupRegionsStr != "" {
		rw.Request.BackupRegions = strings.Split(rw.BackupRegionsStr, ",")
	}
	if rw.AddonsStr != "" {
		rw.Request.Addons = strings.Split(rw.AddonsStr, ",")
	}
}

func (rw *RequestWrapper) addOptions() {
	flag.Int64Var(&rw.Request.MinNodes, "min-nodes", 0, "minimal number of nodes")
	flag.Int64Var(&rw.Request.MaxNodes, "max-nodes", 0, "maximal number of nodes")
	flag.StringVar(&rw.Request.NodeType, "node-type", "", "node type")
	flag.StringVar(&rw.Request.Region, "region", "", "GCP region")
	flag.StringVar(&rw.Request.Zone, "zone", "", "GCP zone")
	flag.StringVar(&rw.Request.Project, "project", "", "GCP project")
	flag.StringVar(&rw.Request.ClusterName, "name", "", "cluster name")
	flag.StringVar(&rw.Request.ReleaseChannel, "release-channel", "", "GKE release channel")
	flag.StringVar(&rw.Request.ResourceType, "resource-type", "", "Boskos Resource Type")
	flag.StringVar(&rw.BackupRegionsStr, "backup-regions", "", "GCP regions as backup, separated by comma")
	flag.StringVar(&rw.AddonsStr, "addons", "", "addons to be added, separated by comma")
	flag.BoolVar(&rw.Request.SkipCreation, "skip-creation", false, "should skip creation or not")
}
