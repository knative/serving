package resources

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func makePod(phase corev1.PodPhase, ready bool, waitingReason string, running bool, terminating bool, readyCondReason string) *corev1.Pod {
	p := &corev1.Pod{Status: corev1.PodStatus{Phase: phase}}
	if terminating {
		p.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}
	}
	// Conditions
	cond := corev1.PodCondition{Type: corev1.PodReady}
	if ready {
		cond.Status = corev1.ConditionTrue
	} else {
		cond.Status = corev1.ConditionFalse
		cond.Reason = readyCondReason
	}
	p.Status.Conditions = []corev1.PodCondition{cond}
	// Container status
	cs := corev1.ContainerStatus{Ready: ready}
	if !ready {
		if running {
			cs.State.Running = &corev1.ContainerStateRunning{}
		} else {
			cs.State.Waiting = &corev1.ContainerStateWaiting{Reason: waitingReason}
		}
	}
	p.Status.ContainerStatuses = []corev1.ContainerStatus{cs}
	return p
}

func TestPodIsStartingClassification(t *testing.T) {
	cases := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{"container creating", makePod(corev1.PodRunning, false, "ContainerCreating", false, false, "ContainersNotReady"), true},
		{"pod initializing", makePod(corev1.PodRunning, false, "PodInitializing", false, false, "ContainersNotReady"), true},
		{"image pull backoff", makePod(corev1.PodRunning, false, "ImagePullBackOff", false, false, "ContainersNotReady"), false},
		{"err image pull", makePod(corev1.PodRunning, false, "ErrImagePull", false, false, "ContainersNotReady"), false},
		{"crashloop", makePod(corev1.PodRunning, false, "CrashLoopBackOff", false, false, "ContainersNotReady"), false},
		{"unknown waiting", makePod(corev1.PodRunning, false, "SomeUnknownReason", false, false, "ContainersNotReady"), false},
		{"running not ready (probe fail)", makePod(corev1.PodRunning, false, "", true, false, "ContainersNotReady"), false},
		{"ready true", makePod(corev1.PodRunning, true, "", false, false, ""), false},
	}
	for _, tc := range cases {
		if got := podIsStarting(tc.pod); got != tc.want {
			t.Errorf("%s: podIsStarting()=%v, want %v", tc.name, got, tc.want)
		}
	}
}
