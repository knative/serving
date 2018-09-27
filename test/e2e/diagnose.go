package e2e

import (
	"fmt"
	"time"

	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/test"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func DiagnoseMeEvery(duration time.Duration, clients *test.Clients, logger *logging.BaseLogger) chan struct{} {
	stopChan := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopChan:
				return
			default:
			}
			DiagnoseMe(clients, logger)
			time.Sleep(duration)
		}
	}()
	return stopChan
}

func DiagnoseMe(clients *test.Clients, logger *logging.BaseLogger) {
	if clients == nil || clients.KubeClient == nil || clients.KubeClient.Kube == nil {
		logger.Errorf(fmt.Sprintf("could not diagnose: nil kube client"))
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
		logger.Errorf(fmt.Sprintf("could not check current pod count: %v", err))
		return
	}
	for _, r := range revs.Items {
		deploymentName := r.Name + "-deployment"
		dep, err := clients.KubeClient.Kube.AppsV1().Deployments(test.ServingNamespace).Get(deploymentName, metav1.GetOptions{})
		if err != nil {
			logger.Errorf(fmt.Sprintf("could not get deployment %v", deploymentName))
			continue
		}
		want := dep.Status.Replicas
		have := dep.Status.ReadyReplicas
		if have != want {
			logger.Infof("deployment %v has %v pods. wants %v.", deploymentName, have, want)
		} else {
			logger.Infof("deployment %v has %v pods.", deploymentName, have)
		}
	}
}

func checkUnschedulablePods(clients *test.Clients, logger *logging.BaseLogger) {
	kube := clients.KubeClient.Kube
	pods, err := kube.CoreV1().Pods(test.ServingNamespace).List(metav1.ListOptions{})
	if err != nil {
		logger.Errorf(fmt.Sprintf("could not check unschedulable pods: %v", err))
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
		logger.Errorf(fmt.Sprintf("%v out of %v pods are unschedulable. insufficient cluster capacity?",
			unschedulablePods, totalPods))
	}
}
