package e2e

import (
	"time"

	"github.com/knative/pkg/test/logging"
	rnames "github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources/names"
	"github.com/knative/serving/test"

	v1types "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DiagnoseMeEvery queries the k8s controller and reports the pod stats to the logger,
// every `duration` period.
func DiagnoseMeEvery(duration time.Duration, clients *test.Clients, logger *logging.BaseLogger) chan struct{} {
	stopChan := make(chan struct{})
	go func() {
		c := time.NewTicker(duration)
		for {
			select {
			case <-c.C:
				diagnoseMe(clients, logger)
				continue
			case <-stopChan:
				c.Stop()
				return
			}
		}
	}()
	return stopChan
}

func diagnoseMe(clients *test.Clients, logger *logging.BaseLogger) {
	if clients == nil || clients.KubeClient == nil || clients.KubeClient.Kube == nil {
		logger.Error("Could not diagnose: nil kube client")
		return
	}

	for _, check := range []func(*test.Clients, *logging.BaseLogger){
		checkCurrentPodCount,
		checkUnschedulablePods,
	} {
		check(clients, logger)
	}
}

func checkCurrentPodCount(clients *test.Clients, logger *logging.BaseLogger) {
	revs, err := clients.ServingClient.Revisions.List(metav1.ListOptions{})
	if err != nil {
		logger.Errorf("Could not check current pod count: %v", err)
		return
	}
	for _, r := range revs.Items {
		deploymentName := rnames.Deployment(&r)
		dep, err := clients.KubeClient.Kube.AppsV1().Deployments(test.ServingNamespace).Get(deploymentName, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("Could not get deployment %v", deploymentName)
			continue
		}
		logger.Infof("Deployment %s has %d pods. wants %d.", deploymentName, dep.Status.Replicas, dep.Status.ReadyReplicas)
	}
}

func checkUnschedulablePods(clients *test.Clients, logger *logging.BaseLogger) {
	kube := clients.KubeClient.Kube
	pods, err := kube.CoreV1().Pods(test.ServingNamespace).List(metav1.ListOptions{})
	if err != nil {
		logger.Errorf("Could not check unschedulable pods: %v", err)
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
		logger.Errorf("%v out of %v pods are unschedulable. Insufficient cluster capacity?", unschedulablePods, totalPods)
	}
}
