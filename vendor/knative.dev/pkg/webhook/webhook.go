/*
Copyright 2017 The Knative Authors

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
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	// Injection stuff
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	kubeinformerfactory "knative.dev/pkg/client/injection/kube/informers/factory"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/system"
	certresources "knative.dev/pkg/webhook/certificates/resources"
)

// Options contains the configuration for the webhook
type Options struct {
	// ServiceName is the service name of the webhook.
	ServiceName string

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

	// StatsReporter reports metrics about the webhook.
	// This will be automatically initialized by the constructor if left uninitialized.
	StatsReporter StatsReporter
}

// AdmissionController provides the interface for different admission controllers
type AdmissionController interface {
	// Path returns the path that this particular admission controller serves on.
	Path() string

	// Admit is the callback which is invoked when an HTTPS request comes in on Path().
	// TODO(mattmoor): This will need to be different for Conversion webhooks, which is something
	// to start thinking about.
	Admit(context.Context, *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse
}

// Webhook implements the external webhook for validation of
// resources and configuration.
type Webhook struct {
	Client               kubernetes.Interface
	Options              Options
	Logger               *zap.SugaredLogger
	admissionControllers map[string][]AdmissionController
	secretlister         corelisters.SecretLister
}

// New constructs a Webhook
func New(
	ctx context.Context,
	admissionControllers []AdmissionController,
) (*Webhook, error) {

	client := kubeclient.Get(ctx)

	// Injection is too aggressive for this case because by simply linking this
	// library we force consumers to have secret access.  If we require that one
	// of the admission controllers' informers *also* require the secret
	// informer, then we can fetch the shared informer factory here and produce
	// a new secret informer from it.
	secretInformer := kubeinformerfactory.Get(ctx).Core().V1().Secrets()

	opts := GetOptions(ctx)
	if opts == nil {
		return nil, errors.New("context must have Options specified")
	}
	logger := logging.FromContext(ctx)

	if opts.StatsReporter == nil {
		reporter, err := NewStatsReporter()
		if err != nil {
			return nil, err
		}
		opts.StatsReporter = reporter
	}

	// Build up a map of paths to admission controllers for routing handlers.
	acs := map[string][]AdmissionController{}
	for _, ac := range admissionControllers {
		acs[ac.Path()] = append(acs[ac.Path()], ac)
	}

	return &Webhook{
		Client:               client,
		Options:              *opts,
		secretlister:         secretInformer.Lister(),
		admissionControllers: acs,
		Logger:               logger,
	}, nil
}

// Run implements the admission controller run loop.
func (ac *Webhook) Run(stop <-chan struct{}) error {
	logger := ac.Logger
	ctx := logging.WithLogger(context.Background(), logger)

	server := &http.Server{
		Handler: ac,
		Addr:    fmt.Sprintf(":%v", ac.Options.Port),
		TLSConfig: &tls.Config{
			GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
				secret, err := ac.secretlister.Secrets(system.Namespace()).Get(ac.Options.SecretName)
				if err != nil {
					return nil, err
				}

				serverKey, ok := secret.Data[certresources.ServerKey]
				if !ok {
					return nil, errors.New("server key missing")
				}
				serverCert, ok := secret.Data[certresources.ServerCert]
				if !ok {
					return nil, errors.New("server cert missing")
				}
				cert, err := tls.X509KeyPair(serverCert, serverKey)
				if err != nil {
					return nil, err
				}
				return &cert, nil
			},
		},
	}

	logger.Info("Found certificates for webhook...")

	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		if err := server.ListenAndServeTLS("", ""); err != nil {
			logger.Errorw("ListenAndServeTLS for admission webhook returned error", zap.Error(err))
			return err
		}
		return nil
	})

	select {
	case <-stop:
		return server.Close()
	case <-ctx.Done():
		return fmt.Errorf("webhook server bootstrap failed %v", ctx.Err())
	}
}

// ServeHTTP implements the external admission webhook for mutating
// serving resources.
func (ac *Webhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var ttStart = time.Now()
	logger := ac.Logger
	logger.Infof("Webhook ServeHTTP request=%#v", r)

	// Verify the content type is accurate.
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		http.Error(w, "invalid Content-Type, want `application/json`", http.StatusUnsupportedMediaType)
		return
	}

	var review admissionv1beta1.AdmissionReview
	if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
		http.Error(w, fmt.Sprintf("could not decode body: %v", err), http.StatusBadRequest)
		return
	}

	logger = logger.With(
		zap.String(logkey.Kind, fmt.Sprint(review.Request.Kind)),
		zap.String(logkey.Namespace, review.Request.Namespace),
		zap.String(logkey.Name, review.Request.Name),
		zap.String(logkey.Operation, fmt.Sprint(review.Request.Operation)),
		zap.String(logkey.Resource, fmt.Sprint(review.Request.Resource)),
		zap.String(logkey.SubResource, fmt.Sprint(review.Request.SubResource)),
		zap.String(logkey.UserInfo, fmt.Sprint(review.Request.UserInfo)))
	ctx := logging.WithLogger(r.Context(), logger)

	cs, ok := ac.admissionControllers[r.URL.Path]
	if !ok {
		http.Error(w, fmt.Sprintf("no admission controller registered for: %s", r.URL.Path), http.StatusBadRequest)
		return
	}

	// TODO(mattmoor): Remove support for multiple AdmissionControllers at
	// the same path after 0.11 cuts.
	// We only TEMPORARILY support multiple AdmissionControllers at the same path because of
	// the issue described here: https://github.com/knative/serving/pull/5947
	// So we only support a single AdmissionController per path returning Patches.
	var response admissionv1beta1.AdmissionReview
	for _, c := range cs {
		reviewResponse := c.Admit(ctx, review.Request)
		logger.Infof("AdmissionReview for %#v: %s/%s response=%#v",
			review.Request.Kind, review.Request.Namespace, review.Request.Name, reviewResponse)

		if !reviewResponse.Allowed {
			response.Response = reviewResponse
			break
		}

		if reviewResponse.PatchType != nil || response.Response == nil {
			response.Response = reviewResponse
		}
	}
	response.Response.UID = review.Request.UID

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("could encode response: %v", err), http.StatusInternalServerError)
		return
	}

	if ac.Options.StatsReporter != nil {
		// Only report valid requests
		ac.Options.StatsReporter.ReportRequest(review.Request, response.Response, time.Since(ttStart))
	}
}

func MakeErrorStatus(reason string, args ...interface{}) *admissionv1beta1.AdmissionResponse {
	result := apierrors.NewBadRequest(fmt.Sprintf(reason, args...)).Status()
	return &admissionv1beta1.AdmissionResponse{
		Result:  &result,
		Allowed: false,
	}
}
