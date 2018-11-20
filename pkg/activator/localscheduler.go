package activator

import (
	"fmt"
	"net/http"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	servingv1alpha1 "github.com/knative/serving/pkg/client/clientset/versioned/typed/serving/v1alpha1"
	"github.com/knative/serving/pkg/localscheduler"
)

type LocalActivator struct {
	servingClient servingv1alpha1.ServingV1alpha1Interface
	kubeClient    *kubernetes.Clientset
	logger        *zap.SugaredLogger
}

func NewLocalActivator(servingClient servingv1alpha1.ServingV1alpha1Interface, kubeClient *kubernetes.Clientset, logger *zap.SugaredLogger) *LocalActivator {
	la := LocalActivator{
		servingClient: servingClient,
		kubeClient:    kubeClient,
		logger:        logger,
	}
	return &la
}

func (ls *LocalActivator) ActiveEndpoint(namespace, name string) ActivationResult {
	revCli := ls.servingClient.Revisions(namespace)
	rev, err := revCli.Get(name, metav1.GetOptions{})
	serviceName, configurationName := GetServiceAndConfigurationLabels(rev)
	if err != nil {
		return ActivationResult{
			Status: http.StatusInternalServerError,
			Error:  fmt.Errorf("Failed to get revision %q/%q: %v", namespace, name, err),
		}
	} else {
		if fqdn, port, err := localscheduler.ScheduleAndWaitForEndpoint(rev, ls.kubeClient, ls.logger); err != nil {
			return ActivationResult{
				Status: http.StatusInternalServerError,
				Error:  fmt.Errorf("Failed to obtain local revision endpoint %q/%q: %v", namespace, name, err),
			}
		} else {
			return ActivationResult{
				Status: http.StatusOK,
				Endpoint: Endpoint{
					FQDN: fqdn,
					Port: port,
				},
				ServiceName:       serviceName,
				ConfigurationName: configurationName,
			}
		}
	}
}

func (ls *LocalActivator) Shutdown() {
}
