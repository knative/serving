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

package actions

import (
	"fmt"
	"log"

	clm "knative.dev/pkg/testutils/clustermanager/e2e-tests"
	"knative.dev/pkg/testutils/clustermanager/prow-cluster-operation/options"
)

// Delete deletes a GKE cluster
func Delete(o *options.RequestWrapper) error {
	o.Request.NeedsCleanup = true
	o.Request.SkipCreation = true

	gkeClient := clm.GKEClient{}
	clusterOps := gkeClient.Setup(o.Request)
	gkeOps := clusterOps.(*clm.GKECluster)
	if err := gkeOps.Acquire(); err != nil || gkeOps.Cluster == nil {
		return fmt.Errorf("failed identifying cluster for cleanup: '%v'", err)
	}
	log.Printf("Identified project %q and cluster %q for removal", gkeOps.Project, gkeOps.Cluster.Name)
	var err error
	if err = gkeOps.Delete(); err != nil {
		return fmt.Errorf("failed deleting cluster: '%v'", err)
	}
	// TODO: uncomment the lines below when previous Delete command becomes
	// async operation
	// // Unset context with best effort. The first command only unsets current
	// // context, but doesn't delete the entry from kubeconfig, and should return it's
	// // context if succeeded, which can be used by the second command to
	// // delete it from kubeconfig
	// if out, err := common.StandardExec("kubectl", "config", "unset", "current-context"); err != nil {
	// 	common.StandardExec("kubectl", "config", "unset", "contexts."+string(out))
	// }

	return nil
}
