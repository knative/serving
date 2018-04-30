/*
Copyright 2018 Google Inc. All Rights Reserved.
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

// route_test contains specs for testing use cases around creating and updating
// Routes.

package conformance

import (
	"errors"
	"fmt"
	"strings"

	"github.com/elafros/elafros/pkg/apis/ela"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"encoding/json"

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/client/clientset/versioned"
	elatyped "github.com/elafros/elafros/pkg/client/clientset/versioned/typed/ela/v1alpha1"
	"github.com/mattbaird/jsonpatch"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	namespaceName = "pizzaplanet"
	image1        = "pizzaplanetv1"
	image2        = "pizzaplanetv2"
	configName    = "prod"
	routeName     = "pizzaplanet"
	ingressName   = routeName + "-ela-ingress"
)

func route() *v1alpha1.Route {
	return &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespaceName,
			Name:      routeName,
		},
		Spec: v1alpha1.RouteSpec{
			Traffic: []v1alpha1.TrafficTarget{
				v1alpha1.TrafficTarget{
					Name:              routeName,
					ConfigurationName: configName,
					Percent:           100,
				},
			},
		},
	}
}

func configuration(imagePath string) *v1alpha1.Configuration {
	return &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespaceName,
			Name:      configName,
		},
		Spec: v1alpha1.ConfigurationSpec{
			RevisionTemplate: v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					Container: corev1.Container{
						Image: imagePath,
					},
				},
			},
		},
	}
}

func isRouteReady() func(r *v1alpha1.Route) (bool, error) {
	return func(r *v1alpha1.Route) (bool, error) {
		return r.Status.IsReady(), nil
	}
}

func allRouteTrafficAtRevision(routeName string, revisionName string) func(r *v1alpha1.Route) (bool, error) {
	return func(r *v1alpha1.Route) (bool, error) {
		if len(r.Status.Traffic) > 0 {
			Expect(r.Status.Traffic).To(HaveLen(1))
			if r.Status.Traffic[0].RevisionName == revisionName {
				Expect(r.Status.Traffic[0].Percent).To(Equal(100))
				Expect(r.Status.Traffic[0].Name).To(Equal(routeName))
				return true, nil
			}
		}
		return false, nil
	}
}

func isRevisionReady(confGen string) func(r *v1alpha1.Revision) (bool, error) {
	return func(r *v1alpha1.Revision) (bool, error) {
		if len(r.Status.Conditions) > 0 {
			Expect(r.Status.Conditions[0].Type).To(Equal(v1alpha1.RevisionConditionType("Ready")))
			if r.Status.Conditions[0].Status == corev1.ConditionStatus("Unknown") {
				Expect(r.Status.Conditions[0].Reason).To(Equal("Deploying"))
			} else {
				Expect(r.Status.Conditions[0].Status).To(Equal(corev1.ConditionStatus("True")))
				Expect(r.Status.Conditions[0].Reason).To(Equal("ServiceReady"))
				Expect(r.Annotations).To(HaveKey(ela.ConfigurationGenerationAnnotationKey))
				Expect(r.Annotations[ela.ConfigurationGenerationAnnotationKey]).To(Equal(confGen))
				return true, nil
			}
		}
		return false, nil
	}
}

func waitForEndpointState(domain string, ingress *v1beta1.Ingress, inState func(body string) (bool, error)) {
	var endpoint, spoofDomain string

	// If the domain that the Route controller is configured to assign to Route.Status.Domain
	// (the domainSuffix) is not resolvable, we need to retrieve the IP of the endpoint and
	// spoof the Host in our requests.
	if !resolvableDomain {
		endpoint = fmt.Sprintf("http://%s", ingress.Status.LoadBalancer.Ingress[0].IP)
		spoofDomain = domain
		// If the domain is resolvable, we can use it directly when we make requests
	} else {
		endpoint = domain
	}

	By("Wait for the endpoint to be up and handling requests")
	// TODO(#348): The ingress endpoint tends to return 503's and 404's
	WaitForRequestToDomainState(endpoint, spoofDomain, []int{503, 404}, inState)
}

func BuildClientConfig(kubeConfigPath string, clusterName string) (*rest.Config, error) {
	overrides := clientcmd.ConfigOverrides{}
	// Override the cluster name if provided.
	if clusterName != "" {
		overrides.Context.Cluster = clusterName
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfigPath},
		&overrides).ClientConfig()
}

var _ = Describe("Route", func() {
	var (
		kubeClientset  *kubernetes.Clientset
		routeClient    elatyped.RouteInterface
		configClient   elatyped.ConfigurationInterface
		revisionClient elatyped.RevisionInterface
		imagePaths     []string
	)
	BeforeSuite(func() {
		cfg, err := BuildClientConfig(kubeconfig, cluster)
		Expect(err).NotTo(HaveOccurred())
		kubeClientset, err = kubernetes.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())

		clientset, err := versioned.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())

		routeClient = clientset.ElafrosV1alpha1().Routes(namespaceName)
		configClient = clientset.ElafrosV1alpha1().Configurations(namespaceName)
		revisionClient = clientset.ElafrosV1alpha1().Revisions(namespaceName)

		imagePaths = append(imagePaths, strings.Join([]string{dockerRepo, image1}, "/"))
		imagePaths = append(imagePaths, strings.Join([]string{dockerRepo, image2}, "/"))
	})

	// Cleanup must be done in `AfterSuite` intead of `AfterEach` because a ctrl-c will stop `AfterEach` from executing (https://github.com/onsi/ginkgo/issues/222).
	AfterSuite(func() {
		if routeClient != nil {
			err := routeClient.Delete(routeName, nil)
			Expect(err).NotTo(HaveOccurred())
		}
		if configClient != nil {
			err := configClient.Delete(configName, nil)
			Expect(err).NotTo(HaveOccurred())
		}
	})

	Context("Deploying an app with a pre-built container", func() {
		It("Creates a route, serves traffic to it, and serves traffic to subsequent revisions", func() {
			By("Creating a new Route")
			_, err := routeClient.Create(route())
			Expect(err).NotTo(HaveOccurred())

			By("Creating Configuration for the Route using the first image")
			_, err = configClient.Create(configuration(imagePaths[0]))
			Expect(err).NotTo(HaveOccurred())

			By("The Configuration will be updated with the Revision after it is created")
			var revisionName string
			WaitForConfigurationState(configClient, configName, func(c *v1alpha1.Configuration) (bool, error) {
				if c.Status.LatestCreatedRevisionName != "" {
					revisionName = c.Status.LatestCreatedRevisionName
					return true, nil
				}
				return false, nil
			})

			By("The Revision will be updated when it is ready to serve traffic")
			WaitForRevisionState(revisionClient, revisionName, isRevisionReady("1"))

			By("The Configuration will be updated when the Revision is ready to serve traffic")
			WaitForConfigurationState(configClient, configName, func(c *v1alpha1.Configuration) (bool, error) {
				return c.Status.LatestReadyRevisionName == revisionName, nil
			})

			By("Once the Configuration has been updated with the Revision, the Route will be updated to route traffic to the Revision")
			WaitForRouteState(routeClient, routeName, allRouteTrafficAtRevision(routeName, revisionName))

			By("Once the Route Ingress has an IP, the Route will be marked as Ready.")
			WaitForRouteState(routeClient, routeName, isRouteReady())
			updatedRoute, err := routeClient.Get(routeName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			ingress, err := kubeClientset.ExtensionsV1beta1().Ingresses(namespaceName).Get(ingressName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(ingress.Status.LoadBalancer.Ingress[0].IP).NotTo(Equal(""))

			By("Make a request to the Revision that is now deployed and serving traffic")
			waitForEndpointState(updatedRoute.Status.Domain, ingress, func(body string) (bool, error) {
				return body == "What a spaceport!", nil
			})

			By("Patch the Configuration to use a new image")
			patches := []jsonpatch.JsonPatchOperation{
				jsonpatch.JsonPatchOperation{
					Operation: "replace",
					Path:      "/spec/revisionTemplate/spec/container/image",
					Value:     imagePaths[1],
				},
			}
			patchBytes, err := json.Marshal(patches)
			Expect(err).NotTo(HaveOccurred())

			newConfig, err := configClient.Patch(configName, types.JSONPatchType, patchBytes, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(newConfig.Generation).To(Equal(int64(0)))
			Expect(newConfig.Spec.Generation).To(Equal(int64(2)))

			By("A new Revision will be made and the Configuration will be updated with it")
			var newRevisionName string
			WaitForConfigurationState(configClient, configName, func(c *v1alpha1.Configuration) (bool, error) {
				if c.Status.LatestCreatedRevisionName != revisionName {
					newRevisionName = c.Status.LatestCreatedRevisionName
					return true, nil
				}
				return false, nil
			})

			By("The new Revision will be updated when it is ready to serve traffic")
			WaitForRevisionState(revisionClient, newRevisionName, isRevisionReady("2"))

			By("The Configuration will be updated to indicate the new revision is ready")
			WaitForConfigurationState(configClient, configName, func(c *v1alpha1.Configuration) (bool, error) {
				return c.Status.LatestReadyRevisionName == newRevisionName, nil
			})

			By("The Route will then immediately send all traffic to the new revision")
			WaitForRouteState(routeClient, routeName, allRouteTrafficAtRevision(routeName, newRevisionName))
			updatedRoute, err = routeClient.Get(routeName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("Wait for the ingress to actually start serving traffic from the newly deployed Revision")
			waitForEndpointState(updatedRoute.Status.Domain, ingress, func(body string) (bool, error) {
				if body == "Re-energize yourself with a slice of pepperoni!" {
					// This is the string we are looking for
					return true, nil
				} else if body == "What a spaceport!" {
					GinkgoWriter.Write([]byte("Connected to previous revision, retrying"))
					return false, nil
				} else {
					s := fmt.Sprintf("Unknown string returned: %s\n", body)
					return true, errors.New(s)
				}
			})
		})
	})
})
