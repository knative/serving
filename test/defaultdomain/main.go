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
	"flag"
	"fmt"
	"strings"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	// Uncomment the following line to load the gcp plugin (only required
	// to authenticate against GKE clusters).
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/knative/pkg/logging"
	"github.com/knative/pkg/system"
	cicfg "github.com/knative/serving/pkg/reconciler/v1alpha1/clusteringress/config"
	routecfg "github.com/knative/serving/pkg/reconciler/v1alpha1/route/config"
)

var (
	masterURL  = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	magicDNS   = flag.String("magic-dns", "", "The hostname for the magic DNS service, e.g. xip.io or nip.io")
)

func main() {
	flag.Parse()
	logger := logging.FromContext(context.TODO()).Named("defaultdomain")

	cfg, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		logger.Fatalw("Error building kubeconfig", zap.Error(err))
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		logger.Fatalw("Error building kube clientset", zap.Error(err))
	}

	// Fetch and parse the domain ConfigMap from the system namespace.
	domainCM, err := kubeClient.CoreV1().ConfigMaps(system.Namespace()).Get(
		routecfg.DomainConfigName, metav1.GetOptions{})
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

	// Fetch and parse the Istio-ClusterIngress ConfigMap from the system namespace.
	istioCM, err := kubeClient.CoreV1().ConfigMaps(system.Namespace()).Get(
		cicfg.IstioConfigName, metav1.GetOptions{})
	if err != nil {
		logger.Fatalw("Error getting ConfigMap", zap.Error(err))
	}
	istioConfig, err := cicfg.NewIstioFromConfigMap(istioCM)
	if err != nil {
		logger.Fatalw("Error parsing ConfigMap", zap.Error(err))
	}

	// Parse the service name to determine which Kubernetes Service resource to fetch.
	serviceURL := istioConfig.IngressGateways[0].ServiceURL
	parts := strings.SplitN(serviceURL, ".", 3)
	if len(parts) != 3 {
		logger.Fatalf("Unexpected service URL form: %v", serviceURL)
	}

	// Use the first two name parts to lookup the Kubernetes Service resource.
	name, ns := parts[0], parts[1]
	gwSvc, err := kubeClient.CoreV1().Services(ns).Get(name, metav1.GetOptions{})
	if err != nil {
		logger.Fatalw("Error getting ConfigMap", zap.Error(err))
	}

	// Walk the list of Ingress entries in the Service's LoadBalancer status
	// and find the first with an assigned IP address.
	ip := ""
	for _, ing := range gwSvc.Status.LoadBalancer.Ingress {
		if ing.IP == "" {
			continue
		}
		ip = ing.IP
		break
	}
	if ip == "" {
		logger.Fatal("Istio gateway does not have an assigned external IP.")
	}

	// Use the IP (assumes IPv4) to set up a magic DNS name under a top-level Magic
	// DNS service like xip.io or nip.io, where:
	//     1.2.3.4.xip.io  ===(magically resolves to)===> 1.2.3.4
	// Add this magic DNS name without a label selector to the ConfigMap,
	// and send it back to the API server.
	domain := fmt.Sprintf("%s.%s", ip, *magicDNS)
	domainCM.Data[domain] = ""
	_, err = kubeClient.CoreV1().ConfigMaps(system.Namespace()).Update(domainCM)
	if err != nil {
		logger.Fatalw("Error updating ConfigMap", zap.Error(err))
	}

	logger.Infof("Updated default domain to: %s", domain)
}
