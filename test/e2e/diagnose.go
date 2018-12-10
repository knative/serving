package e2e

import (
	"time"

	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/test"

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
		deploymentName := r.Name + "-deployment"
		dep, err := clients.KubeClient.Kube.AppsV1().Deployments(test.ServingNamespace).Get(deploymentName, metav1.GetOptions{})
		if err != nil {
			logger.Errorf("Could not get deployment %v", deploymentName)
			continue
		}
		if want, have := dep.Status.Replicas, dep.Status.ReadyReplicas; have != want {
			logger.Infof("Deployment %v has %v pods. wants %v.", deploymentName, have, want)
		} else {
			logger.Infof("Deployment %v has %v pods.", deploymentName, have)
		}
	}
}

func checkUnschedulablePods(clients *test.Clients, logger *logging.BaseLogger) {
	kube := clients.KubeClient.Kube
	pods, err := kube.CoreV1().Pods(test.ServingNamespace).List(metav1.ListOptions{})
	if err != nil {
		logger.Errorf("Could not check unschedulable pods: %v", err)
		return
	}

	totalPods := 0
	unschedulablePods := 0
	for _, p := range pods.Items {
		totalPods++
		for _, c := range p.Status.Conditions {
			if c.Type == "PodScheduled" && c.Status == "False" && c.Reason == "Unschedulable" {
				unschedulablePods++
				break
			}
		}
	}
	if unschedulablePods != 0 {
		logger.Errorf("%v out of %v pods are unschedulable. Insufficient cluster capacity?", unschedulablePods, totalPods)
	}
}
