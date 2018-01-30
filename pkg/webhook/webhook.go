/*
Copyright 2017 Google Inc. All Rights Reserved.
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

package webhook

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/google/elafros/pkg/apis/ela"
	"github.com/google/elafros/pkg/apis/ela/v1alpha1"

	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	"github.com/mattbaird/jsonpatch"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	clientadmissionregistrationv1beta1 "k8s.io/client-go/kubernetes/typed/admissionregistration/v1beta1"
)

const (
	elafrosAPIVersion = "v1alpha1"
	secretServerKey   = "server-key.pem"
	secretServerCert  = "server-cert.pem"
	secretCACert      = "ca-cert.pem"
	// TODO: Could these come from somewhere else.
	elaSystemNamespace   = "ela-system"
	elaWebhookDeployment = "ela-webhook"
)

var deploymentKind = v1beta1.SchemeGroupVersion.WithKind("Deployment")

// ControllerOptions contains the configuration for the webhook
type ControllerOptions struct {
	// WebhookName is the name of the webhook we create to handle
	// mutations before they get stored in the storage.
	WebhookName string

	// ServiceName is the service name of the webhook.
	ServiceName string

	// ServiceNamespace is the namespace of the webhook service.
	ServiceNamespace string

	// SecretName is the name of k8s secret that contains the webhook
	// server key/cert and corresponding CA cert that signed them. The
	// server key/cert are used to serve the webhook and the CA cert
	// is provided to k8s apiserver during admission controller
	// registration.
	SecretName string

	// Port where the webhook is served. Per k8s admission
	// registration requirements this should be 443 unless there is
	// only a single port for the service.
	Port int

	// RegistrationDelay controls how long admission registration
	// occurs after the webhook is started. This is used to avoid
	// potential races where registration completes and k8s apiserver
	// invokes the webhook before the HTTP server is started.
	RegistrationDelay time.Duration
}

// AdmissionController implements the external admission webhook for validation of
// pilot configuration.
type AdmissionController struct {
	client  kubernetes.Interface
	options ControllerOptions
}

// GetAPIServerExtensionCACert gets the Kubernetes aggregate apiserver
// client CA cert used by validator.
//
// NOTE: this certificate is provided kubernetes. We do not control
// its name or location.
func getAPIServerExtensionCACert(cl kubernetes.Interface) ([]byte, error) {
	const name = "extension-apiserver-authentication"
	c, err := cl.CoreV1().ConfigMaps(metav1.NamespaceSystem).Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	pem, ok := c.Data["requestheader-client-ca-file"]
	if !ok {
		return nil, fmt.Errorf("cannot find ca.crt in %v: ConfigMap.Data is %#v", name, c.Data)
	}
	return []byte(pem), nil
}

// MakeTLSConfig makes a TLS configuration suitable for use with the server
func makeTLSConfig(serverCert, serverKey, caCert []byte) (*tls.Config, error) {
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	cert, err := tls.X509KeyPair(serverCert, serverKey)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caCertPool,
		ClientAuth:   tls.NoClientCert,
		// Note on GKE there apparently is no client cert sent, so this
		// does not work on GKE.
		// TODO: make this into a configuration option.
		//		ClientAuth:   tls.RequireAndVerifyClientCert,
	}, nil
}

func getOrGenerateKeyCertsFromSecret(client kubernetes.Interface, name, namespace string) (serverKey, serverCert, caCert []byte, err error) {
	secret, err := client.CoreV1().Secrets(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, nil, nil, err
		}
		glog.Infof("Did not find existing secret, creating one")
		newSecret, err := generateSecret(name, namespace)
		if err != nil {
			return nil, nil, nil, err
		}
		secret, err = client.CoreV1().Secrets(namespace).Create(newSecret)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, nil, nil, err
		}
		// Ok, so something else might have created, try fetching it one more time
		secret, err = client.CoreV1().Secrets(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			return nil, nil, nil, err
		}
	}

	var ok bool
	if serverKey, ok = secret.Data[secretServerKey]; !ok {
		return nil, nil, nil, errors.New("server key missing")
	}
	if serverCert, ok = secret.Data[secretServerCert]; !ok {
		return nil, nil, nil, errors.New("server cert missing")
	}
	if caCert, ok = secret.Data[secretCACert]; !ok {
		return nil, nil, nil, errors.New("ca cert missing")
	}
	return serverKey, serverCert, caCert, nil
}

// NewAdmissionController creates a new instance of the admission webhook controller.
func NewAdmissionController(client kubernetes.Interface, options ControllerOptions) (*AdmissionController, error) {
	return &AdmissionController{
		client:  client,
		options: options,
	}, nil
}

func configureCerts(client kubernetes.Interface, options *ControllerOptions) (*tls.Config, []byte, error) {
	apiServerCACert, err := getAPIServerExtensionCACert(client)
	if err != nil {
		return nil, nil, err
	}
	serverKey, serverCert, caCert, err := getOrGenerateKeyCertsFromSecret(
		client, options.SecretName, options.ServiceNamespace)
	if err != nil {
		return nil, nil, err
	}
	tlsConfig, err := makeTLSConfig(serverCert, serverKey, apiServerCACert)
	if err != nil {
		return nil, nil, err
	}
	return tlsConfig, caCert, nil
}

// Run implements the admission controller run loop.
func (ac *AdmissionController) Run(stop <-chan struct{}) {
	tlsConfig, caCert, err := configureCerts(ac.client, &ac.options)
	if err != nil {
		glog.Infof("Could not configure admission webhook certs: %v", err)
		return
	}

	server := &http.Server{
		Handler:   ac,
		Addr:      fmt.Sprintf(":%v", ac.options.Port),
		TLSConfig: tlsConfig,
	}

	glog.Info("Found certificates for webhook...")
	if ac.options.RegistrationDelay != 0 {
		glog.Infof("Delaying admission webhook registration for %v", ac.options.RegistrationDelay)
	}

	select {
	case <-time.After(ac.options.RegistrationDelay):
		cl := ac.client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations()
		if err := ac.register(cl, caCert); err != nil {
			glog.Infof("Failed to register webhook: %v", err)
			return
		}
		defer func() {
			if err := ac.unregister(cl); err != nil {
				glog.Infof("Failed to unregister webhook: %v", err)
			}
		}()
		glog.Info("Successfully registered webhook")
	case <-stop:
		return
	}

	go func() {
		if err := server.ListenAndServeTLS("", ""); err != nil {
			glog.Infof("ListenAndServeTLS for admission webhook returned error: %v", err)
		}
	}()
	<-stop
	server.Close() // nolint: errcheck
}

// Unregister unregisters the external admission webhook
func (ac *AdmissionController) unregister(client clientadmissionregistrationv1beta1.MutatingWebhookConfigurationInterface) error {
	glog.Info("Exiting..")
	return nil
}

// Register registers the external admission webhook for pilot
// configuration types.

func (ac *AdmissionController) register(client clientadmissionregistrationv1beta1.MutatingWebhookConfigurationInterface, caCert []byte) error { // nolint: lll
	// TODO(vaikas): Make this generic
	resources := []string{"revisiontemplates"}

	webhook := &admissionregistrationv1beta1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: ac.options.WebhookName,
		},
		Webhooks: []admissionregistrationv1beta1.Webhook{
			{
				Name: ac.options.WebhookName,
				Rules: []admissionregistrationv1beta1.RuleWithOperations{{
					Operations: []admissionregistrationv1beta1.OperationType{
						admissionregistrationv1beta1.Create,
						admissionregistrationv1beta1.Update,
					},
					Rule: admissionregistrationv1beta1.Rule{
						APIGroups:   []string{ela.GroupName},
						APIVersions: []string{elafrosAPIVersion},
						Resources:   resources,
					},
				}},
				ClientConfig: admissionregistrationv1beta1.WebhookClientConfig{
					Service: &admissionregistrationv1beta1.ServiceReference{
						Namespace: ac.options.ServiceNamespace,
						Name:      ac.options.ServiceName,
					},
					CABundle: caCert,
				},
			},
		},
	}

	// Set the owner to our deployment
	deployment, err := ac.client.ExtensionsV1beta1().Deployments(elaSystemNamespace).Get(elaWebhookDeployment, metav1.GetOptions{})
	if err != nil {
		glog.Fatalf("Failed to fetch our deployment: %s", err)
		return err
	}
	deploymentRef := metav1.NewControllerRef(deployment, deploymentKind)
	webhook.OwnerReferences = append(webhook.OwnerReferences, *deploymentRef)

	// Try to create the webhook and if it already exists, use it.
	_, err = client.Create(webhook)
	if err != nil {
		if !apierrors.IsAlreadyExists(err) {
			glog.Fatalf("Failed to create a webhook: %s", err)
			return err
		}
		glog.Infof("Webhook already exists")
	} else {
		glog.Infof("Created a webhook")
	}
	return nil
}

// ServeHTTP implements the external admission webhook for mutating
// ela resources.
func (ac *AdmissionController) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	glog.Infof("Webhook ServeHTTP request=%#v", r)

	var body []byte
	if r.Body != nil {
		data, err := ioutil.ReadAll(r.Body)
		if err != nil {
			glog.Warningf("Failed to read incoming Body: %s", err)
			http.Error(w, fmt.Sprintf("could not read incoming body: %v", err), http.StatusInternalServerError)
		}
		body = data
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		http.Error(w, "invalid Content-Type, want `application/json`", http.StatusUnsupportedMediaType)
		return
	}

	var review admissionv1beta1.AdmissionReview
	if err := json.Unmarshal(body, &review); err != nil {
		http.Error(w, fmt.Sprintf("could not decode body: %v", err), http.StatusBadRequest)
		return
	}

	reviewResponse := ac.admit(review.Request)
	response := admissionv1beta1.AdmissionReview{}

	if reviewResponse != nil {
		response.Response = reviewResponse
		response.Response.UID = review.Request.UID
	}

	glog.Infof("AdmissionReview for %s: %v/%v response=%v",
		review.Request.Kind, review.Request.Namespace, review.Request.Name, reviewResponse)

	resp, err := json.Marshal(response)
	if err != nil {
		http.Error(w, fmt.Sprintf("could encode response: %v", err), http.StatusInternalServerError)
		return
	}
	if _, err := w.Write(resp); err != nil {
		http.Error(w, fmt.Sprintf("could write response: %v", err), http.StatusInternalServerError)
		return
	}
}

func (ac *AdmissionController) admit(request *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
	makeErrorStatus := func(reason string, args ...interface{}) *admissionv1beta1.AdmissionResponse {
		result := apierrors.NewBadRequest(fmt.Sprintf(reason, args...)).Status()
		return &admissionv1beta1.AdmissionResponse{
			Result: &result,
		}
	}

	switch request.Operation {
	case admissionv1beta1.Create, admissionv1beta1.Update:
	default:
		glog.Infof("Unhandled webhook operation, letting it through %v", request.Operation)
		return &admissionv1beta1.AdmissionResponse{Allowed: true}
	}

	var obj v1alpha1.RevisionTemplate
	glog.Infof("INCOMING OBJECT: %v", string(request.Object.Raw))
	if err := yaml.Unmarshal(request.Object.Raw, &obj); err != nil {
		return makeErrorStatus("cannot decode incoming object: %v", err)
	}

	var oldObj v1alpha1.RevisionTemplate
	if len(request.OldObject.Raw) != 0 {
		if err := yaml.Unmarshal(request.OldObject.Raw, &oldObj); err != nil {
			return makeErrorStatus("cannot decode old incoming object: %v", err)
		}
	}

	patchBytes, err := ac.mutate(&oldObj, &obj)
	if err != nil {
		return makeErrorStatus("mutation failed: %v", err)
	}
	glog.Infof("PatchBytes: %v", string(patchBytes))

	return &admissionv1beta1.AdmissionResponse{
		Patch:   patchBytes,
		Allowed: true,
		PatchType: func() *admissionv1beta1.PatchType {
			pt := admissionv1beta1.PatchTypeJSONPatch
			return &pt
		}(),
	}
}

func (ac *AdmissionController) mutate(old *v1alpha1.RevisionTemplate, new *v1alpha1.RevisionTemplate) ([]byte, error) {
	var patches []jsonpatch.JsonPatchOperation

	// Update the generation
	updateGeneration(&patches, old, new)

	// TODO(vaikas): Call into appropriate object specific mutators

	return json.Marshal(patches)
}

// updateGeneration sets the generation by following this logic:
// if there's no old object, it's create, set generation to 1
// if there's an old object and spec has changed, set generation to old.generation + 1
// appends the patch to patches if changes are necessary.
// TODOD: Generation does not work correctly with CRD. They are scrubbed
// by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
// So, we add Generation here. Once that gets fixed, remove this and use
// ObjectMeta.Generation instead.
func updateGeneration(patches *[]jsonpatch.JsonPatchOperation, old *v1alpha1.RevisionTemplate, new *v1alpha1.RevisionTemplate) error {
	if old == nil || old.Spec.Generation == 0 {
		glog.Infof("Creating an object, setting generation to 1")
		*patches = append(*patches, jsonpatch.JsonPatchOperation{
			Operation: "add",
			Path:      "/spec/generation",
			Value:     1,
		})
		return nil
	}
	// Diff the specs and see if there are differences
	oldJSON, err := json.Marshal(old.Spec)
	if err != nil {
		return err
	}
	newJSON, err := json.Marshal(new.Spec)
	if err != nil {
		return err
	}
	specPatches, e := jsonpatch.CreatePatch([]byte(oldJSON), []byte(newJSON))
	if e != nil {
		fmt.Printf("Error creating JSON patch:%v", e)
		return err
	}
	if len(specPatches) > 0 {
		specPatchesJSON, err := json.Marshal(specPatches)
		if err != nil {
			glog.Infof("Failed to marshal spec patches: %s", err)
			return err
		}
		glog.Infof("Specs differ:\n%+v\n", string(specPatchesJSON))
		*patches = append(*patches, jsonpatch.JsonPatchOperation{
			Operation: "replace",
			Path:      "/spec/generation",
			Value:     old.Spec.Generation + 1,
		})
		return nil
	}
	glog.Infof("No changes in the spec, not bumping generation...")
	return nil
}

func generateSecret(name, namespace string) (*corev1.Secret, error) {
	serverKey, serverCert, caCert, err := CreateCerts()
	if err != nil {
		return nil, err
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			secretServerKey:  serverKey,
			secretServerCert: serverCert,
			secretCACert:     caCert,
		},
	}, nil
}
