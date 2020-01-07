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

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/client/clientset/versioned"
	"knative.dev/serving/pkg/network"
	routecfg "knative.dev/serving/pkg/reconciler/route/config"
)

var (
	masterURL  = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	magicDNS   = flag.String("magic-dns", "", "The hostname for the magic DNS service, e.g. xip.io or nip.io")
)

const (
	// Interval to poll for objects.
	pollInterval = 10 * time.Second
	// How long to wait for objects.
	waitTimeout = 20 * time.Minute
	appName     = "default-domain"
)

func clientsFromFlags() (*kubernetes.Clientset, *versioned.Clientset, error) {
	cfg, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		return nil, nil, fmt.Errorf("error building kubeconfig: %w", err)
	}
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("error building kube clientset: %w", err)
	}
	client, err := versioned.NewForConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("error building serving clientset: %w", err)
	}
	return kubeClient, client, nil
}

func lookupConfigMap(kubeClient *kubernetes.Clientset, name string) (*corev1.ConfigMap, error) {
	return kubeClient.CoreV1().ConfigMaps(system.Namespace()).Get(name, metav1.GetOptions{})
}

func findGatewayAddress(kubeclient *kubernetes.Clientset, client *versioned.Clientset) (*corev1.LoadBalancerIngress, error) {
	netCM, err := lookupConfigMap(kubeclient, network.ConfigName)
	if err != nil {
		return nil, err
	}
	netCfg, err := network.NewConfigFromConfigMap(netCM)
	if err != nil {
		return nil, err
	}

	// Create a KIngress that points at that Service
	ing, err := client.NetworkingV1alpha1().Ingresses(system.Namespace()).Create(&v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "default-domain-",
			Namespace:    system.Namespace(),
			Annotations: map[string]string{
				networking.IngressClassAnnotationKey: netCfg.DefaultIngressClass,
			},
		},
		Spec: v1alpha1.IngressSpec{
			Rules: []v1alpha1.IngressRule{{
				Hosts:      []string{os.Getenv("POD_NAME") + ".default-domain.invalid"},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
				HTTP: &v1alpha1.HTTPIngressRuleValue{
					Paths: []v1alpha1.HTTPIngressPath{{
						Splits: []v1alpha1.IngressBackendSplit{{
							IngressBackend: v1alpha1.IngressBackend{
								ServiceName:      "default-domain-service",
								ServiceNamespace: system.Namespace(),
								ServicePort:      intstr.FromInt(80),
							},
						}},
					}},
				},
			}},
		},
	})
	if err != nil {
		return nil, err
	}
	defer client.NetworkingV1alpha1().Ingresses(system.Namespace()).Delete(ing.Name, &metav1.DeleteOptions{})

	// Wait for the Ingress to be Ready.
	if err := wait.PollImmediate(pollInterval, waitTimeout, func() (done bool, err error) {
		ing, err = client.NetworkingV1alpha1().Ingresses(system.Namespace()).Get(
			ing.Name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		return ing.Status.IsReady(), nil
	}); err != nil {
		return nil, err
	}
	if len(ing.Status.PublicLoadBalancer.Ingress) == 0 {
		return nil, errors.New("ingress has no public load balancers in status")
	}

	// We expect an ingress LB with the form foo.bar.svc.cluster.local (though
	// we aren't strictly sensitive to the suffix, this is just illustrative).
	internalDomain := ing.Status.PublicLoadBalancer.Ingress[0].DomainInternal
	parts := strings.SplitN(internalDomain, ".", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("ingress public load balancer had unexpected shape: %q", internalDomain)
	}
	name, namespace := parts[0], parts[1]

	// Wait for the Ingress Service to have an external IP.
	var svc *corev1.Service
	if err := wait.PollImmediate(pollInterval, waitTimeout, func() (done bool, err error) {
		svc, err = kubeclient.CoreV1().Services(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		return len(svc.Status.LoadBalancer.Ingress) != 0, nil
	}); err != nil {
		return nil, err
	}
	return &svc.Status.LoadBalancer.Ingress[0], nil
}

func main() {
	flag.Parse()
	logger := logging.FromContext(context.Background()).Named(appName)
	defer logger.Sync()

	kubeClient, client, err := clientsFromFlags()
	if err != nil {
		logger.Fatalw("Error building kube clientset", zap.Error(err))
	}

	// Fetch and parse the domain ConfigMap from the system namespace.
	domainCM, err := lookupConfigMap(kubeClient, routecfg.DomainConfigName)
	if err != nil {
		logger.Fatalw("Error getting ConfigMap", zap.Error(err))
	}
	domainConfig, err := routecfg.NewDomainFromConfigMap(domainCM)
	if err != nil {
		logger.Fatalw("Error parsing ConfigMap", zap.Error(err))
	}
	// If there is a catch-all domain configured, then bail out (successfully) here.
	defaultDomain := domainConfig.LookupDomainForLabels(map[string]string{})
	if defaultDomain != routecfg.DefaultDomain {
		logger.Infof("Domain is configured as: %v", defaultDomain)
		return
	}

	// Start an HTTP Server
	h := network.NewProbeHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server := http.Server{Addr: ":8080", Handler: h}
	go server.ListenAndServe()

	// Determine the address of the gateway service.
	address, err := findGatewayAddress(kubeClient, client)
	if err != nil {
		logger.Fatalw("Error finding gateway address", zap.Error(err))
	}
	if address.IP == "" {
		logger.Info("Gateway has a domain instead of IP address -- leaving default domain config intact")
		return
	}

	// Use the IP (assumes IPv4) to set up a magic DNS name under a top-level Magic
	// DNS service like xip.io or nip.io, where:
	//     1.2.3.4.xip.io  ===(magically resolves to)===> 1.2.3.4
	// Add this magic DNS name without a label selector to the ConfigMap,
	// and send it back to the API server.
	domain := fmt.Sprintf("%s.%s", address.IP, *magicDNS)
	domainCM.Data[domain] = ""
	if _, err = kubeClient.CoreV1().ConfigMaps(system.Namespace()).Update(domainCM); err != nil {
		logger.Fatalw("Error updating ConfigMap", zap.Error(err))
	}

	logger.Infof("Updated default domain to: %s", domain)
	server.Shutdown(context.Background())
}
