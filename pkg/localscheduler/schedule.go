package localscheduler

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/queue"
)

// TODO(greghaynes): This should be shared with pkg/reconciler/v1alpha1/revision/resources
const (
	knativeRevisionEnvVariableKey      = "K_REVISION"
	knativeConfigurationEnvVariableKey = "K_CONFIGURATION"
	knativeServiceEnvVariableKey       = "K_SERVICE"

	varLogVolumeName = "varlog"

	userContainerName = "user-container"

	userPortName    = "user-port"
	userPort        = 8080
	userPortEnvName = "PORT"
)

var (
	userContainerCPU = resource.MustParse("400m")

	varLogVolume = corev1.Volume{
		Name: varLogVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}

	varLogVolumeMount = corev1.VolumeMount{
		Name:      varLogVolumeName,
		MountPath: "/var/log",
	}

	userPorts = []corev1.ContainerPort{{
		Name:          userPortName,
		ContainerPort: int32(userPort),
	}}

	// Expose containerPort as env PORT.
	userEnv = corev1.EnvVar{
		Name:  userPortEnvName,
		Value: strconv.Itoa(userPort),
	}

	userResources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU: userContainerCPU,
		},
	}
)

func getKnativeEnvVar(rev *v1alpha1.Revision) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  knativeRevisionEnvVariableKey,
			Value: rev.Name,
		},
		{
			Name:  knativeConfigurationEnvVariableKey,
			Value: rev.Labels[serving.ConfigurationLabelKey],
		},
		{
			Name:  knativeServiceEnvVariableKey,
			Value: rev.Labels[serving.ServiceLabelKey],
		},
	}
}

func createPodSpec(rev *v1alpha1.Revision) *corev1.PodSpec {
	userContainer := rev.Spec.Container.DeepCopy()
	// Adding or removing an overwritten corev1.Container field here? Don't forget to
	// update the validations in pkg/webhook.validateContainer.
	userContainer.Name = userContainerName
	userContainer.Resources = userResources
	userContainer.Ports = userPorts
	userContainer.VolumeMounts = append(userContainer.VolumeMounts, varLogVolumeMount)
	userContainer.Env = append(userContainer.Env, userEnv)
	userContainer.Env = append(userContainer.Env, getKnativeEnvVar(rev)...)
	// Prefer imageDigest from revision if available
	if rev.Status.ImageDigest != "" {
		userContainer.Image = rev.Status.ImageDigest
	}

	// If the client provides probes, we should fill in the port for them.
	rewriteUserProbe(userContainer.ReadinessProbe)
	rewriteUserProbe(userContainer.LivenessProbe)

	podSpec := &corev1.PodSpec{
		Containers: []corev1.Container{
			*userContainer,
		},
		Volumes:            []corev1.Volume{varLogVolume},
		ServiceAccountName: rev.Spec.ServiceAccountName,
	}

	return podSpec
}

func rewriteUserProbe(p *corev1.Probe) {
	if p == nil {
		return
	}
	switch {
	case p.HTTPGet != nil:
		// For HTTP probes, we route them through the queue container
		// so that we know the queue proxy is ready/live as well.
		p.HTTPGet.Port = intstr.FromInt(queue.RequestQueuePort)
	case p.TCPSocket != nil:
		p.TCPSocket.Port = intstr.FromInt(userPort)
	}
}

func schedule(rev *v1alpha1.Revision, podName, nonce string, logger *zap.SugaredLogger) error {
	s := json.NewYAMLSerializer(json.DefaultMetaFactory, scheme.Scheme,
		scheme.Scheme)

	ps := createPodSpec(rev)
	podDef := corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: rev.ObjectMeta.Namespace,
			Annotations: map[string]string{
				"sidecar.istio.io/inject": "false",
			},
			Labels: map[string]string{
				"localpod-nonce": nonce,
			},
		},
		Spec: *ps,
	}

	f, err := os.Create(fmt.Sprintf("/etc/localpods/%s", podName))
	if err != nil {
		logger.Errorf("Opening local pod definition (%q): %v", rev.Name, err)
	}
	defer f.Close()
	err = s.Encode(&podDef, f)
	if err != nil {
		logger.Errorf("Writing local pod definition (%q): %v", rev.Name, err)
	}
	logger.Info("Write out local schedul pod spec")
	return nil
}

func ScheduleAndWaitForEndpoint(rev *v1alpha1.Revision, kubeClient *kubernetes.Clientset, logger *zap.SugaredLogger) (string, int32, error) {
	podName := fmt.Sprintf("localpod-%s-%s", rev.ObjectMeta.Namespace, rev.Name)

	// TODO(greghaynes) Use a real nonce here or come up with a better way to find our local pod
	if err := schedule(rev, podName, "1234", logger); err != nil {
		return "", 0, err
	}

	podCli := kubeClient.Core().Pods(rev.ObjectMeta.Namespace)
	wi, err := podCli.Watch(metav1.ListOptions{})
	if err != nil {
		return "", 0, err
	}
	defer wi.Stop()

	logger.Infof("Waiting for pod %q/%q to become ready", rev.ObjectMeta.Namespace, podName)
	ch := wi.ResultChan()
	for {
		select {
		case <-time.After(120 * time.Second):
			return "", 0, fmt.Errorf("Timed out waiting for locally scheduled pod (%q) to become ready.", podName)
		case event := <-ch:
			if pod, ok := event.Object.(*corev1.Pod); ok {
				if pod.ObjectMeta.Labels != nil && pod.ObjectMeta.Labels["localpod-nonce"] == "1234" {
					logger.Debug("Got event for our pod (%v/%v). Status = %v, IP = %v.", pod.ObjectMeta.Namespace, pod.ObjectMeta.Name, pod.Status.Phase, pod.Status.PodIP)
					if pod.Status.Phase != corev1.PodRunning {
						logger.Debug("Ignoring pod due to not runing status.")
						continue
					}
					if pod.Status.PodIP == "" {
						return "", 0, fmt.Errorf("Locally scheduled pod is running but podIP is not set")
					}
					logger.Debug("Returning running pod.")
					return pod.Status.PodIP, 8080, nil
				} else {
					logger.Debug("Ignoring event for pod %q/%q", pod.ObjectMeta.Namespace, pod.Name)
				}
			} else {
				return "", 0, fmt.Errorf("Unexpected pod event type while waiting for locally scheduled pod: %v.", event)
			}
		}
	}
}
