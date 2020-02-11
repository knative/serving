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
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/tracker"
)

// BaseReconciler helps implement controller.Reconciler for Binding resources.
type BaseReconciler struct {
	// The GVR of the "primary key" resource for this reconciler.
	// This is used along with the DynamicClient for updating the status
	// and managing finalizers of the resources being reconciled.
	GVR schema.GroupVersionResource

	// Get is a callback that fetches the Bindable with the provided name
	// and namespace (for this GVR).
	Get func(namespace string, name string) (Bindable, error)

	// WithContext is a callback that infuses the context supplied to
	// Do/Undo with additional context to enable them to complete their
	// respective tasks.
	WithContext BindableContext

	// DynamicClient is used to patch subjects and apply mutations to
	// Bindable resources (determined by GVR) to reflect status updates.
	DynamicClient dynamic.Interface

	// Factory is used for producing listers for the object references we
	// encounter.
	Factory duck.InformerFactory

	// The tracker builds an index of what resources are watching other
	// resources so that we can immediately react to changes to changes in
	// tracked resources.
	Tracker tracker.Interface

	// Recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	Recorder record.EventRecorder
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*BaseReconciler)(nil)

// Reconcile implements controller.Reconciler
func (r *BaseReconciler) Reconcile(ctx context.Context, key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		logging.FromContext(ctx).Errorf("invalid resource key: %s", key)
		return nil
	}

	// Get the resource with this namespace/name.
	original, err := r.Get(namespace, name)
	if apierrs.IsNotFound(err) {
		// The resource may no longer exist, in which case we stop processing.
		logging.FromContext(ctx).Errorf("resource %q no longer exists", key)
		return nil
	} else if err != nil {
		return err
	}
	// Don't modify the informers copy.
	resource := original.DeepCopyObject().(Bindable)

	// Reconcile this copy of the resource and then write back any status
	// updates regardless of whether the reconciliation errored out.
	reconcileErr := r.reconcile(ctx, resource)
	if equality.Semantic.DeepEqual(original.GetBindingStatus(), resource.GetBindingStatus()) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if err = r.UpdateStatus(ctx, resource); err != nil {
		logging.FromContext(ctx).Warnw("Failed to update resource status", zap.Error(err))
		r.Recorder.Eventf(resource, corev1.EventTypeWarning, "UpdateFailed",
			"Failed to update status for %q: %v", resource.GetName(), err)
		return err
	}
	if reconcileErr != nil {
		r.Recorder.Event(resource, corev1.EventTypeWarning, "InternalError", reconcileErr.Error())
	}
	return reconcileErr
}

// reconcile is a reference implementation of a simple Binding flow, it and
// Reconcile may be overridden and the exported helpers used to implement a
// customized reconciliation flow.
func (r *BaseReconciler) reconcile(ctx context.Context, fb Bindable) error {
	if fb.GetDeletionTimestamp() != nil {
		// Check for a DeletionTimestamp.  If present, elide the normal
		// reconcile logic and do our finalizer handling.
		return r.ReconcileDeletion(ctx, fb)
	}
	// Make sure that our conditions have been initialized.
	fb.GetBindingStatus().InitializeConditions()

	// Make sure that the resource has a Finalizer configured, which
	// enables us to undo our binding upon deletion.
	if err := r.EnsureFinalizer(ctx, fb); err != nil {
		return err
	}

	// Perform our Binding's Do() method on the subject(s) of the Binding.
	if err := r.ReconcileSubject(ctx, fb, fb.Do); err != nil {
		return err
	}

	// Update the observed generation once we have successfully reconciled
	// our spec.
	fb.GetBindingStatus().SetObservedGeneration(fb.GetGeneration())
	return nil
}

// ReconcileDeletion handles reconcile a resource that is being deleted, which
// amounts to properly finalizing the resource.
func (r *BaseReconciler) ReconcileDeletion(ctx context.Context, fb Bindable) error {
	// If we are not the controller finalizing this resource, then we
	// are done.
	if !r.IsFinalizing(ctx, fb) {
		return nil
	}

	// If it is our turn to finalize the Binding, then first undo the effect
	// of our Binding on the resource.
	logging.FromContext(ctx).Infof("Removing the binding for %s", fb.GetName())
	if err := r.ReconcileSubject(ctx, fb, fb.Undo); apierrs.IsNotFound(err) || apierrs.IsForbidden(err) {
		// If the subject has been deleted, then there is nothing to undo.
	} else if err != nil {
		return err
	}

	// Once the Binding has been undone, remove our finalizer allowing the
	// Binding resource's deletion to progress.
	return r.RemoveFinalizer(ctx, fb)
}

// IsFinalizing determines whether it is our reconciler's turn to finalize a
// resource in the process of being deleted.  This means that our finalizer is
// at the head of the metadata.finalizers list.
func (r *BaseReconciler) IsFinalizing(ctx context.Context, fb kmeta.Accessor) bool {
	return len(fb.GetFinalizers()) != 0 && fb.GetFinalizers()[0] == r.GVR.GroupResource().String()
}

// EnsureFinalizer makes sure that the provided resource has a finalizer in the
// form of this BaseReconciler's GVR's stringified GroupResource.
func (r *BaseReconciler) EnsureFinalizer(ctx context.Context, fb kmeta.Accessor) error {
	// If it has the finalizer, then we're done.
	finalizers := sets.NewString(fb.GetFinalizers()...)
	if finalizers.Has(r.GVR.GroupResource().String()) {
		return nil
	}

	// If it doesn't have our finalizer, then synthesize a patch to add it.
	patch, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"finalizers":      append(fb.GetFinalizers(), r.GVR.GroupResource().String()),
			"resourceVersion": fb.GetResourceVersion(),
		},
	})
	if err != nil {
		return err
	}

	// ... and apply it.
	_, err = r.DynamicClient.Resource(r.GVR).Namespace(fb.GetNamespace()).Patch(fb.GetName(),
		types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

// RemoveFinalizer is the dual of EnsureFinalizer, it removes our finalizer from
// the Binding resource
func (r *BaseReconciler) RemoveFinalizer(ctx context.Context, fb kmeta.Accessor) error {
	logging.FromContext(ctx).Info("Removing Finalizer")

	// Synthesize a patch removing our finalizer from the head of the
	// finalizer list.
	patch, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"finalizers":      fb.GetFinalizers()[1:],
			"resourceVersion": fb.GetResourceVersion(),
		},
	})
	if err != nil {
		return err
	}

	// ... and apply it.
	_, err = r.DynamicClient.Resource(r.GVR).Namespace(fb.GetNamespace()).Patch(fb.GetName(),
		types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

// ReconcileSubject handles applying the provided Binding "mutation" (Do or
// Undo) to the Binding's subject(s).
func (r *BaseReconciler) ReconcileSubject(ctx context.Context, fb Bindable, mutation Mutation) error {
	// Access the subject of our Binding and have the tracker queue this
	// Bindable whenever it changes.
	subject := fb.GetSubject()
	if err := r.Tracker.TrackReference(subject, fb); err != nil {
		logging.FromContext(ctx).Errorf("Error tracking subject %v: %v", subject, err)
		return err
	}

	// Determine the GroupVersionResource of the subject reference
	gv, err := schema.ParseGroupVersion(subject.APIVersion)
	if err != nil {
		logging.FromContext(ctx).Errorf("Error parsing GroupVersion %v: %v", subject.APIVersion, err)
		return err
	}
	gvr := apis.KindToResource(gv.WithKind(subject.Kind))

	// Use the GVR of the subject(s) to get ahold of a lister that we can
	// use to fetch our PodSpecable resources.
	_, lister, err := r.Factory.Get(gvr)
	if err != nil {
		logging.FromContext(ctx).Errorf("Error getting a lister for resource '%+v': %v", gvr, err)
		fb.GetBindingStatus().MarkBindingUnavailable("SubjectUnavailable", err.Error())
		return err
	}

	// Based on the type of subject reference, build up a list of referents.
	var referents []*duckv1.WithPod
	if subject.Name != "" {
		// If name is specified, then fetch it from the lister and turn
		// it into a singleton list.
		psObj, err := lister.ByNamespace(subject.Namespace).Get(subject.Name)
		if apierrs.IsNotFound(err) {
			fb.GetBindingStatus().MarkBindingUnavailable("SubjectMissing", err.Error())
			return err
		} else if err != nil {
			return fmt.Errorf("error fetching Pod Speccable %v: %v", subject, err)
		}
		referents = append(referents, psObj.(*duckv1.WithPod))
	} else {
		// Otherwise, the subject is referenced by selector, so compile
		// the selector and pass it to the lister.
		selector, err := metav1.LabelSelectorAsSelector(subject.Selector)
		if err != nil {
			return err
		}
		psObjs, err := lister.ByNamespace(subject.Namespace).List(selector)
		if err != nil {
			return fmt.Errorf("error fetching Pod Speccable %v: %v", subject, err)
		}
		// Type cast the returned resources into our referent list.
		for _, psObj := range psObjs {
			referents = append(referents, psObj.(*duckv1.WithPod))
		}
	}

	// Callback into the user's code to setup the context with additional
	// information needed to perform the mutation.
	if r.WithContext != nil {
		ctx, err = r.WithContext(ctx, fb)
		if err != nil {
			return err
		}
	}

	// For each of the referents, apply the mutation.
	eg := errgroup.Group{}
	for _, ps := range referents {
		ps := ps
		eg.Go(func() error {
			// Do the binding to the pod speccable.
			orig := ps.DeepCopy()
			mutation(ctx, ps)

			// If nothing changed, then bail early.
			if equality.Semantic.DeepEqual(orig, ps) {
				return nil
			}

			// If we encountered changes, then synthesize and apply
			// a patch.
			patchBytes, err := duck.CreateBytePatch(orig, ps)
			if err != nil {
				return err
			}

			// TODO(mattmoor): This might fail because a binding changed after
			// a Job started or completed, which can be fine.  Consider treating
			// certain error codes as acceptable.
			_, err = r.DynamicClient.Resource(gvr).Namespace(ps.Namespace).Patch(
				ps.Name, types.JSONPatchType, patchBytes, metav1.PatchOptions{})
			if err != nil {
				return fmt.Errorf("failed binding subject %s: %w", ps.Name, err)
			}
			return nil
		})
	}

	// Based on the success of the referent binding, update the Binding's readiness.
	if err := eg.Wait(); err != nil {
		fb.GetBindingStatus().MarkBindingUnavailable("BindingFailed", err.Error())
		return err
	}
	fb.GetBindingStatus().MarkBindingAvailable()
	return nil
}

// UpdateStatus updates the status of the resource.  Caller is responsible for
// checking for semantic differences before calling.
func (r *BaseReconciler) UpdateStatus(ctx context.Context, desired Bindable) error {
	actual, err := r.Get(desired.GetNamespace(), desired.GetName())
	if err != nil {
		logging.FromContext(ctx).Errorf("Error fetching actual: %v", err)
		return err
	}

	// Convert to unstructured for use with the dynamic client.
	ua, err := duck.ToUnstructured(actual)
	if err != nil {
		logging.FromContext(ctx).Errorf("Error converting actual: %v", err)
		return err
	}
	ud, err := duck.ToUnstructured(desired)
	if err != nil {
		logging.FromContext(ctx).Errorf("Error converting desired: %v", err)
		return err
	}

	// One last check that status changed.
	actualStatus := ua.Object["status"]
	desiredStatus := ud.Object["status"]
	if reflect.DeepEqual(actualStatus, desiredStatus) {
		return nil
	}

	// Copy the status over to the refetched resource to avoid updating
	// anything other than status.
	forUpdate := ua
	forUpdate.Object["status"] = desiredStatus
	_, err = r.DynamicClient.Resource(r.GVR).Namespace(desired.GetNamespace()).UpdateStatus(
		forUpdate, metav1.UpdateOptions{})
	return err
}
