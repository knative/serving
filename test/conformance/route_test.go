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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/client/clientset/versioned"
	elatyped "github.com/elafros/elafros/pkg/client/clientset/versioned/typed/ela/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/kubernetes/typed/extensions/v1beta1"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	namespaceName = "pizzaplanet"
	image1        = "ela-conformance-test-v1"
	image2        = "ela-conformance-test-v2"
	domain        = "weregoingtopizzaplanet.com"
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
			DomainSuffix: domain,
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
			Template: v1alpha1.Revision{
				Spec: v1alpha1.RevisionSpec{
					Container: &corev1.Container{
						Image: imagePath,
						ReadinessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/healthz",
									Port: intstr.IntOrString{
										Type:   intstr.Int,
										IntVal: 8080,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func namespace() *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
		},
	}
}

func createNamespace(namespaceClient typedcorev1.NamespaceInterface) {
	// A previous test run may have created this namespace already. We aren't cleaning it up
	// because it can take up to 3 minutes to delete.
	n, err := namespaceClient.List(metav1.ListOptions{})
	for _, item := range n.Items {
		if item.Name == namespaceName {
			return
		}
	}
	_, err = namespaceClient.Create(namespace())
	Expect(err).NotTo(HaveOccurred())
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

func isRevisionReady(revisionName string) func(r *v1alpha1.Revision) (bool, error) {
	return func(r *v1alpha1.Revision) (bool, error) {
		if len(r.Status.Conditions) > 0 {
			Expect(r.Status.Conditions[0].Type).To(Equal(v1alpha1.RevisionConditionType("Ready")))
			if r.Status.Conditions[0].Status == "False" {
				Expect(r.Status.Conditions[0].Reason).To(Equal("Deploying"))
			} else {
				Expect(r.Status.Conditions[0].Status).To(Equal(corev1.ConditionStatus("True")))
				Expect(r.Status.Conditions[0].Reason).To(Equal("ServiceReady"))
				return true, nil
			}
		}
		return false, nil
	}
}

var _ = Describe("Route", func() {
	var (
		namespaceClient typedcorev1.NamespaceInterface
		ingressClient   v1beta1.IngressInterface

		routeClient    elatyped.RouteInterface
		configClient   elatyped.ConfigurationInterface
		revisionClient elatyped.RevisionInterface
		imagePaths     []string
	)
	BeforeSuite(func() {
		cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		Expect(err).NotTo(HaveOccurred())
		kubeClientset, err := kubernetes.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())

		namespaceClient = kubeClientset.CoreV1().Namespaces()
		createNamespace(namespaceClient)

		ingressClient = kubeClientset.ExtensionsV1beta1().Ingresses(namespaceName)

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
			createdRoute, err := routeClient.Create(route())
			Expect(err).NotTo(HaveOccurred())
			Expect(createdRoute.Generation).To(Equal(int64(0)))
			Expect(createdRoute.Spec.Generation).To(Equal(int64(1)))

			By("Creating Configuration for the Route using the first image")
			createdConfig, err := configClient.Create(configuration(imagePaths[0]))
			Expect(err).NotTo(HaveOccurred())
			Expect(createdConfig.Generation).To(Equal(int64(0)))
			Expect(createdConfig.Spec.Generation).To(Equal(int64(1)))

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
			WaitForRevisionState(revisionClient, revisionName, isRevisionReady(revisionName))

			By("The Configuration will be updated when the Revision is ready to serve traffic")
			WaitForConfigurationState(configClient, configName, func(c *v1alpha1.Configuration) (bool, error) {
				return c.Status.LatestReadyRevisionName == revisionName, nil
			})

			By("Once the Configuration has been updated with the Revision, the Route will be updated to route traffic to the Revision")
			WaitForRouteState(routeClient, routeName, allRouteTrafficAtRevision(routeName, revisionName))

			// TODO: The test needs to be able to make a request without needing to retrieve
			// the ingress manually (i.e. by using the domain directly)
			var endpoint string
			By("Wait for the ingress loadbalancer address to be set")
			WaitForIngressState(ingressClient, ingressName, func(i *apiv1beta1.Ingress) (bool, error) {
				if len(i.Status.LoadBalancer.Ingress) > 0 {
					endpoint = fmt.Sprintf("http://%s", i.Status.LoadBalancer.Ingress[0].IP)
					return true, nil
				}
				return false, nil
			})

			By("Make a request to the Revision that is now deployed and serving traffic")
			// TODO: The ingress endpoint tends to return 503's and 404's after an initial deployment of a Revision.
			// Open a bug for this? We're even using readinessProbe, seems like this shouldn't happen.
			WaitForIngressRequestToDomainState(endpoint, domain, []int{503, 404}, func(body string) (bool, error) {
				return body == "What a spaceport!", nil
			})

			By("Patch the Configuration to use a new image")
			patchConfig, err := GetChangedConfigurationBytes(configuration(imagePaths[0]), configuration(imagePaths[1]))
			Expect(err).NotTo(HaveOccurred())
			newConfig, err := configClient.Patch(configName, types.MergePatchType, patchConfig, "")
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
			WaitForRevisionState(revisionClient, revisionName, isRevisionReady(newRevisionName))

			By("The Configuration will be updated to indicate the new revision is ready")
			WaitForConfigurationState(configClient, configName, func(c *v1alpha1.Configuration) (bool, error) {
				return c.Status.LatestReadyRevisionName == newRevisionName, nil
			})

			By("The Route will then immediately send all traffic to the new revision")
			WaitForRouteState(routeClient, routeName, allRouteTrafficAtRevision(routeName, newRevisionName))

			By("Wait for the ingress to actually start serving traffic from the newly deployed Revision")
			WaitForIngressRequestToDomainState(endpoint, domain, []int{503, 404}, func(body string) (bool, error) {
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
