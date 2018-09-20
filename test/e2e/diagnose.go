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
			if diagnosis := DiagnoseMe(clients); len(diagnosis) != 0 {
				logger.Errorf("diagnosis: %v", diagnosis)
			}
			time.Sleep(duration)
		}
	}()
	return stopChan
}

func DiagnoseMe(clients *test.Clients) []string {
	diagnosis := make([]string, 0)
	if msg := checkUnschedulablePods(clients); msg != "" {
		diagnosis = append(diagnosis, msg)
	}
	return diagnosis
}

func checkUnschedulablePods(clients *test.Clients) string {
	if clients == nil || clients.KubeClient == nil || clients.KubeClient.Kube == nil {
		return fmt.Sprintf("could not check unschedulable pods: nil kube client")
	}
	kube := clients.KubeClient.Kube
	pods, err := kube.CoreV1().Pods("serving-tests").List(metav1.ListOptions{})
	if err != nil {
		return fmt.Sprintf("could not check unschedulable pods: %v", err)
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
		return fmt.Sprintf("%v out of %v pods are unschedulable. insufficient cluster capacity?",
			unschedulablePods, totalPods)
	}
	return ""
}
