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

package psbinding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/markbates/inflect"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	admissionlisters "k8s.io/client-go/listers/admissionregistration/v1beta1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmp"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	"knative.dev/pkg/webhook"
	certresources "knative.dev/pkg/webhook/certificates/resources"
)

// ReconcilerOptions is a function to modify the Reconciler.
type ReconcilerOption func(*Reconciler)

// WithSelector specifies the selector for the webhook.
func WithSelector(s metav1.LabelSelector) ReconcilerOption {
	return func(r *Reconciler) {
		r.selector = s
	}
}

func NewReconciler(
	name, path, secretName string,
	client kubernetes.Interface,
	mwhLister admissionlisters.MutatingWebhookConfigurationLister,
	secretLister corelisters.SecretLister,
	withContext BindableContext,
	options ...ReconcilerOption,
) *Reconciler {
	r := &Reconciler{
		Name:        name,
		HandlerPath: path,
		SecretName:  secretName,

		// This is the user-provided context-decorator, which allows
		// them to infuse the context passed to Do/Undo.
		WithContext: withContext,

		Client:       client,
		MWHLister:    mwhLister,
		SecretLister: secretLister,
		selector:     ExclusionSelector, // Use ExclusionSelector by default.
	}

	// Apply options.
	for _, opt := range options {
		opt(r)
	}

	return r
}

// Reconciler implements an AdmissionController for altering PodSpecable
// resources that are the subject of a particular type of Binding.
// The two key methods are:
//  1. reconcileMutatingWebhook: which enumerates all of the Bindings and
//     compiles a list of resource types that should be intercepted by our
//     webhook.  It also builds an index that can be used to efficiently
//     handle Admit requests.
//  2. Admit: which leverages the index built by the Reconciler to apply
//     mutations to resources.
type Reconciler struct {
	Name        string
	HandlerPath string
	SecretName  string

	Client       kubernetes.Interface
	MWHLister    admissionlisters.MutatingWebhookConfigurationLister
	SecretLister corelisters.SecretLister
	ListAll      ListAll

	// WithContext is a callback that infuses the context supplied to
	// Do/Undo with additional context to enable them to complete their
	// respective tasks.
	WithContext BindableContext

	selector metav1.LabelSelector

	// lock protects access to exact and inexact
	lock    sync.RWMutex
	exact   exactMatcher
	inexact inexactMatcher
}

var _ controller.Reconciler = (*Reconciler)(nil)
var _ webhook.AdmissionController = (*Reconciler)(nil)

// We need to specifically exclude our deployment(s) from consideration, but this provides a way
// of excluding other things as well.
var (
	ExclusionSelector = metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{{
			Key:      duck.BindingExcludeLabel,
			Operator: metav1.LabelSelectorOpNotIn,
			Values:   []string{"true"},
		}},
		// TODO(mattmoor): Consider also having a GVR-based one, e.g.
		//    foobindings.blah.knative.dev/exclude: "true"
	}
	InclusionSelector = metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{{
			Key:      duck.BindingIncludeLabel,
			Operator: metav1.LabelSelectorOpIn,
			Values:   []string{"true"},
		}},
		// TODO(mattmoor): Consider also having a GVR-based one, e.g.
		//    foobindings.blah.knative.dev/include: "true"
	}
)

// Reconcile implements controller.Reconciler
func (ac *Reconciler) Reconcile(ctx context.Context, key string) error {
	// Look up the webhook secret, and fetch the CA cert bundle.
	secret, err := ac.SecretLister.Secrets(system.Namespace()).Get(ac.SecretName)
	if err != nil {
		logging.FromContext(ctx).Errorf("Error fetching secret: %v", err)
		return err
	}
	caCert, ok := secret.Data[certresources.CACert]
	if !ok {
		return fmt.Errorf("secret %q is missing %q key", ac.SecretName, certresources.CACert)
	}

	// Reconcile the webhook configuration.
	return ac.reconcileMutatingWebhook(ctx, caCert)
}

// Path implements AdmissionController
func (ac *Reconciler) Path() string {
	return ac.HandlerPath
}

// Admit implements AdmissionController
func (ac *Reconciler) Admit(ctx context.Context, request *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
	switch request.Operation {
	case admissionv1beta1.Create, admissionv1beta1.Update:
	default:
		logging.FromContext(ctx).Infof("Unhandled webhook operation, letting it through %v", request.Operation)
		return &admissionv1beta1.AdmissionResponse{Allowed: true}
	}

	orig := &duckv1.WithPod{}
	decoder := json.NewDecoder(bytes.NewBuffer(request.Object.Raw))
	if err := decoder.Decode(&orig); err != nil {
		return webhook.MakeErrorStatus("unable to decode object: %v", err)
	}

	// Look up the Bindable for this resource.
	fb := func() Bindable {
		ac.lock.RLock()
		defer ac.lock.RUnlock()

		// Always try to find an exact match first.
		if sb, ok := ac.exact.Get(exactKey{
			Group:     request.Kind.Group,
			Kind:      request.Kind.Kind,
			Namespace: request.Namespace,
			Name:      orig.Name,
		}); ok {
			return sb
		}

		// Next look for inexact matches.
		if sb, ok := ac.inexact.Get(inexactKey{
			Group:     request.Kind.Group,
			Kind:      request.Kind.Kind,
			Namespace: request.Namespace,
		}, labels.Set(orig.Labels)); ok {
			return sb
		}
		return nil
	}()
	if fb == nil {
		// This doesn't apply!
		return &admissionv1beta1.AdmissionResponse{Allowed: true}
	}

	// Callback into the user's code to setup the context with additional
	// information needed to perform the mutation.
	if ac.WithContext != nil {
		var err error
		ctx, err = ac.WithContext(ctx, fb)
		if err != nil {
			return webhook.MakeErrorStatus("unable to setup binding context: %v", err)
		}
	}

	// Mutate a copy according to the deletion state of the Bindable.
	delta := orig.DeepCopy()
	if fb.GetDeletionTimestamp() != nil {
		fb.Undo(ctx, delta)
	} else {
		fb.Do(ctx, delta)
	}

	// Synthesize a patch from the changes and return it in our AdmissionResponse
	patchBytes, err := duck.CreateBytePatch(orig, delta)
	if err != nil {
		return webhook.MakeErrorStatus("unable to create patch with binding: %v", err)
	}
	return &admissionv1beta1.AdmissionResponse{
		Patch:   patchBytes,
		Allowed: true,
		PatchType: func() *admissionv1beta1.PatchType {
			pt := admissionv1beta1.PatchTypeJSONPatch
			return &pt
		}(),
	}
}

func (ac *Reconciler) reconcileMutatingWebhook(ctx context.Context, caCert []byte) error {
	// Build a deduplicated list of all of the GVKs we see.
	gks := map[schema.GroupKind]sets.String{}

	// When reconciling the webhook, enumerate all of the bindings, so that
	// we can index them to efficiently respond to webhook requests.
	fbs, err := ac.ListAll()
	if err != nil {
		return err
	}
	exact := make(exactMatcher, len(fbs))
	inexact := make(inexactMatcher, len(fbs))
	for _, fb := range fbs {
		ref := fb.GetSubject()
		gv, err := schema.ParseGroupVersion(ref.APIVersion)
		if err != nil {
			return err
		}
		gk := schema.GroupKind{
			Group: gv.Group,
			Kind:  ref.Kind,
		}
		set := gks[gk]
		if set == nil {
			set = sets.NewString()
		}
		set.Insert(gv.Version)
		gks[gk] = set

		if ref.Name != "" {
			exact.Add(exactKey{
				Group:     gk.Group,
				Kind:      gk.Kind,
				Namespace: ref.Namespace,
				Name:      ref.Name,
			}, fb)
		} else {
			selector, err := metav1.LabelSelectorAsSelector(ref.Selector)
			if err != nil {
				return err
			}
			inexact.Add(inexactKey{
				Group:     gk.Group,
				Kind:      gk.Kind,
				Namespace: ref.Namespace,
			}, selector, fb)
		}
	}

	// Update our indices
	func() {
		ac.lock.Lock()
		defer ac.lock.Unlock()
		ac.exact = exact
		ac.inexact = inexact
	}()

	var rules []admissionregistrationv1beta1.RuleWithOperations
	for gk, versions := range gks {
		plural := strings.ToLower(inflect.Pluralize(gk.Kind))

		rules = append(rules, admissionregistrationv1beta1.RuleWithOperations{
			Operations: []admissionregistrationv1beta1.OperationType{
				admissionregistrationv1beta1.Create,
				admissionregistrationv1beta1.Update,
			},
			Rule: admissionregistrationv1beta1.Rule{
				APIGroups:   []string{gk.Group},
				APIVersions: versions.List(),
				Resources:   []string{plural + "/*"},
			},
		})
	}

	// Sort the rules by Group, Version, Kind so that things are deterministically ordered.
	sort.Slice(rules, func(i, j int) bool {
		lhs, rhs := rules[i], rules[j]
		if lhs.APIGroups[0] != rhs.APIGroups[0] {
			return lhs.APIGroups[0] < rhs.APIGroups[0]
		}
		if lhs.APIVersions[0] != rhs.APIVersions[0] {
			return lhs.APIVersions[0] < rhs.APIVersions[0]
		}
		return lhs.Resources[0] < rhs.Resources[0]
	})

	configuredWebhook, err := ac.MWHLister.Get(ac.Name)
	if err != nil {
		return fmt.Errorf("error retrieving webhook: %v", err)
	}
	webhook := configuredWebhook.DeepCopy()

	// Use the "Equivalent" match policy so that we don't need to enumerate versions for same-types.
	// This is only supported by 1.15+ clusters.
	matchPolicy := admissionregistrationv1beta1.Equivalent

	for i, wh := range webhook.Webhooks {
		if wh.Name != webhook.Name {
			continue
		}
		webhook.Webhooks[i].MatchPolicy = &matchPolicy
		webhook.Webhooks[i].Rules = rules
		webhook.Webhooks[i].NamespaceSelector = &ac.selector
		webhook.Webhooks[i].ObjectSelector = &ac.selector // 1.15+ only
		webhook.Webhooks[i].ClientConfig.CABundle = caCert
		if webhook.Webhooks[i].ClientConfig.Service == nil {
			return fmt.Errorf("missing service reference for webhook: %s", wh.Name)
		}
		webhook.Webhooks[i].ClientConfig.Service.Path = ptr.String(ac.Path())
	}

	if ok, err := kmp.SafeEqual(configuredWebhook, webhook); err != nil {
		return fmt.Errorf("error diffing webhooks: %v", err)
	} else if !ok {
		logging.FromContext(ctx).Info("Updating webhook")
		mwhclient := ac.Client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations()
		if _, err := mwhclient.Update(webhook); err != nil {
			return fmt.Errorf("failed to update webhook: %v", err)
		}
	} else {
		logging.FromContext(ctx).Info("Webhook is valid")
	}
	return nil
}
