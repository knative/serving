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
	secretinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/secret"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/system"
	certresources "knative.dev/pkg/webhook/certificates/resources"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
)

var (
	errMissingNewObject = errors.New("the new object may not be nil")
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

	// RegistrationDelay controls how long admission registration
	// occurs after the webhook is started. This is used to avoid
	// potential races where registration completes and k8s apiserver
	// invokes the webhook before the HTTP server is started.
	RegistrationDelay time.Duration

	// StatsReporter reports metrics about the webhook.
	// This will be automatically initialized by the constructor if left uninitialized.
	StatsReporter StatsReporter
}

// AdmissionController provides the interface for different admission controllers
type AdmissionController interface {
	// Path returns the path that this particular admission controller serves on.
	Path() string

	// Admit is the callback which is invoked when an HTTPS request comes in on Path().
	Admit(context.Context, *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse

	// Register is called at startup to give the AdmissionController a chance to
	// register with the API Server.
	Register(context.Context, kubernetes.Interface, []byte) error
}

// Webhook implements the external webhook for validation of
// resources and configuration.
type Webhook struct {
	Client               kubernetes.Interface
	Options              Options
	Logger               *zap.SugaredLogger
	admissionControllers map[string]AdmissionController
	secretlister         corelisters.SecretLister
}

// New constructs a Webhook
func New(
	ctx context.Context,
	admissionControllers []AdmissionController,
) (*Webhook, error) {

	client := kubeclient.Get(ctx)
	secretInformer := secretinformer.Get(ctx)
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

	acs := make(map[string]AdmissionController, len(admissionControllers))
	for _, ac := range admissionControllers {
		if _, ok := acs[ac.Path()]; ok {
			return nil, fmt.Errorf("admission controller with conflicting path: %q", ac.Path())
		}
		acs[ac.Path()] = ac
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
	ctx := logging.WithLogger(context.TODO(), logger)

	// TODO(mattmoor): Separate out the certificate creation process and use listers
	// to fetch this from the secret below.
	_, _, caCert, err := getOrGenerateKeyCertsFromSecret(ctx, ac.Client, &ac.Options)
	if err != nil {
		return err
	}

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
	if ac.Options.RegistrationDelay != 0 {
		logger.Infof("Delaying admission webhook registration for %v", ac.Options.RegistrationDelay)
	}

	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		select {
		case <-time.After(ac.Options.RegistrationDelay):
			// Wait an initial delay before registering
		case <-stop:
			return nil
		}
		// Register the webhook, and then periodically check that it is up to date.
		for {
			for _, c := range ac.admissionControllers {
				if err := c.Register(ctx, ac.Client, caCert); err != nil {
					logger.Errorw("failed to register webhook", zap.Error(err))
					return err
				}
			}
			logger.Info("Successfully registered webhook")
			select {
			case <-time.After(10 * time.Minute):
			case <-stop:
				return nil
			}
		}
	})
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

	if _, ok := ac.admissionControllers[r.URL.Path]; !ok {
		http.Error(w, fmt.Sprintf("no admission controller registered for: %s", r.URL.Path), http.StatusBadRequest)
		return
	}

	c := ac.admissionControllers[r.URL.Path]
	reviewResponse := c.Admit(ctx, review.Request)
	var response admissionv1beta1.AdmissionReview
	if reviewResponse != nil {
		response.Response = reviewResponse
		response.Response.UID = review.Request.UID
	}

	logger.Infof("AdmissionReview for %#v: %s/%s response=%#v",
		review.Request.Kind, review.Request.Namespace, review.Request.Name, reviewResponse)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("could encode response: %v", err), http.StatusInternalServerError)
		return
	}

	if ac.Options.StatsReporter != nil {
		// Only report valid requests
		ac.Options.StatsReporter.ReportRequest(review.Request, response.Response, time.Since(ttStart))
	}
}

func getOrGenerateKeyCertsFromSecret(ctx context.Context, client kubernetes.Interface,
	options *Options) (serverKey, serverCert, caCert []byte, err error) {
	logger := logging.FromContext(ctx)
	secret, err := client.CoreV1().Secrets(system.Namespace()).Get(options.SecretName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, nil, nil, err
		}
		logger.Info("Did not find existing secret, creating one")
		newSecret, err := certresources.MakeSecret(
			ctx, options.SecretName, system.Namespace(), options.ServiceName)
		if err != nil {
			return nil, nil, nil, err
		}
		secret, err = client.CoreV1().Secrets(newSecret.Namespace).Create(newSecret)
		if err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return nil, nil, nil, err
			}
			// OK, so something else might have created, try fetching it instead.
			secret, err = client.CoreV1().Secrets(system.Namespace()).Get(options.SecretName, metav1.GetOptions{})
			if err != nil {
				return nil, nil, nil, err
			}
		}
	}

	var ok bool
	if serverKey, ok = secret.Data[certresources.ServerKey]; !ok {
		return nil, nil, nil, errors.New("server key missing")
	}
	if serverCert, ok = secret.Data[certresources.ServerCert]; !ok {
		return nil, nil, nil, errors.New("server cert missing")
	}
	if caCert, ok = secret.Data[certresources.CACert]; !ok {
		return nil, nil, nil, errors.New("ca cert missing")
	}
	return serverKey, serverCert, caCert, nil
}

func makeErrorStatus(reason string, args ...interface{}) *admissionv1beta1.AdmissionResponse {
	result := apierrors.NewBadRequest(fmt.Sprintf(reason, args...)).Status()
	return &admissionv1beta1.AdmissionResponse{
		Result:  &result,
		Allowed: false,
	}
}
