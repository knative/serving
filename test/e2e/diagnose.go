package e2e

import (
	"sync"
	"testing"
	"time"

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
	for _, r := range revs.Items {
		deploymentName := r.Name
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
