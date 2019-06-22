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
package e2e

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/knative/pkg/test/helpers"
	rnames "github.com/knative/serving/pkg/reconciler/revision/resources/names"
	"github.com/knative/serving/test"

	v1types "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type stateChecker struct {
	stopChan chan struct{}
	ticker   *time.Ticker
	wg       *sync.WaitGroup
}

// startDiagnosis creates and starts the periodic checker that checks and logs
// k8s pod stats every duration period.
// use stateChecker.stop() to stop the checker before test exit to avoid panics in the logs.
func startDiagnosis(t *testing.T, clients *test.Clients, duration time.Duration) *stateChecker {
	st := &stateChecker{
		stopChan: make(chan struct{}),
		ticker:   time.NewTicker(duration),
		wg:       &sync.WaitGroup{},
	}
	st.wg.Add(1)
	go func() {
		defer st.wg.Done()
		for {
			select {
			case <-st.ticker.C:
				diagnoseMe(t, clients)
			case <-st.stopChan:
				st.ticker.Stop()
				t.Log("Stopping diagnose-me checker.")
				return
			}
		}
	}()
	return st
}

func (sc *stateChecker) stop() {
	close(sc.stopChan)
	sc.wg.Wait()
}

func diagnoseMe(t *testing.T, clients *test.Clients) {
	if clients == nil || clients.KubeClient == nil || clients.KubeClient.Kube == nil {
		t.Log("Could not diagnose: nil kube client")
		return
	}

	for _, check := range []func(*testing.T, *test.Clients){
		checkCurrentPodCount,
		checkUnschedulablePods,
	} {
		check(t, clients)
	}
}

func checkCurrentPodCount(t *testing.T, clients *test.Clients) {
	revs, err := clients.ServingAlphaClient.Revisions.List(metav1.ListOptions{})
	if err != nil {
		t.Logf("%v: could not check current pod count: %v", time.Now(), err)
		return
	}
	prefix := helpers.ObjectPrefixForTest(t)
	for _, r := range revs.Items {
		if !strings.HasPrefix(r.Name, prefix) {
			continue
		}
		deploymentName := rnames.Deployment(&r)
		dep, err := clients.KubeClient.Kube.AppsV1().Deployments(test.ServingNamespace).Get(deploymentName, metav1.GetOptions{})
		if err != nil {
			t.Logf("Could not get deployment %s", deploymentName)
			continue
		}
		t.Logf("%v: deployment %s has %d pods, want: %d.", time.Now(), deploymentName, dep.Status.Replicas, dep.Status.ReadyReplicas)
	}
}

func checkUnschedulablePods(t *testing.T, clients *test.Clients) {
	kube := clients.KubeClient.Kube
	pods, err := kube.CoreV1().Pods(test.ServingNamespace).List(metav1.ListOptions{})
	if err != nil {
		t.Logf("%v: could not check unschedulable pods: %v", time.Now(), err)
		return
	}

	totalPods := len(pods.Items)
	unschedulablePods := 0
	for _, p := range pods.Items {
		for _, c := range p.Status.Conditions {
			if c.Type == v1types.PodScheduled && c.Status == v1types.ConditionFalse && c.Reason == v1types.PodReasonUnschedulable {
				unschedulablePods++
				break
			}
		}
	}
	if unschedulablePods != 0 {
		t.Logf("%v: %d out of %d pods are unschedulable. Insufficient cluster capacity?", time.Now(), unschedulablePods, totalPods)
	}
}
