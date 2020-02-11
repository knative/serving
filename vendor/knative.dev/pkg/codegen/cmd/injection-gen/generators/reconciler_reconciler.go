/*
Copyright 2020 The Knative Authors.

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

package generators

import (
	"io"

	"k8s.io/gengo/generator"
	"k8s.io/gengo/namer"
	"k8s.io/gengo/types"
	"k8s.io/klog"
)

// reconcilerReconcilerGenerator produces a reconciler struct for the given type.
type reconcilerReconcilerGenerator struct {
	generator.DefaultGen
	outputPackage  string
	imports        namer.ImportTracker
	typeToGenerate *types.Type
	clientsetPkg   string
	listerName     string
	listerPkg      string
}

var _ generator.Generator = (*reconcilerReconcilerGenerator)(nil)

func (g *reconcilerReconcilerGenerator) Filter(c *generator.Context, t *types.Type) bool {
	// Only process the type for this generator.
	return t == g.typeToGenerate
}

func (g *reconcilerReconcilerGenerator) Namers(c *generator.Context) namer.NameSystems {
	return namer.NameSystems{
		"raw": namer.NewRawNamer(g.outputPackage, g.imports),
	}
}

func (g *reconcilerReconcilerGenerator) Imports(c *generator.Context) (imports []string) {
	imports = append(imports, g.imports.ImportLines()...)
	return
}

func (g *reconcilerReconcilerGenerator) GenerateType(c *generator.Context, t *types.Type, w io.Writer) error {
	sw := generator.NewSnippetWriter(w, c, "{{", "}}")

	klog.V(5).Infof("processing type %v", t)

	m := map[string]interface{}{
		"type": t,

		"controllerImpl": c.Universe.Type(types.Name{
			Package: "knative.dev/pkg/controller",
			Name:    "Impl",
		}),
		"controllerReconciler": c.Universe.Type(types.Name{
			Package: "knative.dev/pkg/controller",
			Name:    "Reconciler",
		}),
		"corev1EventSource": c.Universe.Function(types.Name{
			Package: "k8s.io/api/core/v1",
			Name:    "EventSource",
		}),
		"corev1EventTypeNormal": c.Universe.Type(types.Name{
			Package: "k8s.io/api/core/v1",
			Name:    "EventTypeNormal",
		}),
		"corev1EventTypeWarning": c.Universe.Type(types.Name{
			Package: "k8s.io/api/core/v1",
			Name:    "EventTypeWarning",
		}),
		"reconcilerEvent":           c.Universe.Type(types.Name{Package: "knative.dev/pkg/reconciler", Name: "Event"}),
		"reconcilerReconcilerEvent": c.Universe.Type(types.Name{Package: "knative.dev/pkg/reconciler", Name: "ReconcilerEvent"}),

		"reconcilerRetryUpdateConflicts": c.Universe.Function(types.Name{Package: "knative.dev/pkg/reconciler", Name: "RetryUpdateConflicts"}),
		// Deps
		"clientsetInterface": c.Universe.Type(types.Name{Name: "Interface", Package: g.clientsetPkg}),
		"resourceLister":     c.Universe.Type(types.Name{Name: g.listerName, Package: g.listerPkg}),
		// K8s types
		"recordEventRecorder": c.Universe.Type(types.Name{Name: "EventRecorder", Package: "k8s.io/client-go/tools/record"}),
		// methods
		"loggingFromContext": c.Universe.Function(types.Name{
			Package: "knative.dev/pkg/logging",
			Name:    "FromContext",
		}),
		"cacheSplitMetaNamespaceKey": c.Universe.Function(types.Name{
			Package: "k8s.io/client-go/tools/cache",
			Name:    "SplitMetaNamespaceKey",
		}),
		"retryRetryOnConflict": c.Universe.Function(types.Name{
			Package: "k8s.io/client-go/util/retry",
			Name:    "RetryOnConflict",
		}),
		"apierrsIsNotFound": c.Universe.Function(types.Name{
			Package: "k8s.io/apimachinery/pkg/api/errors",
			Name:    "IsNotFound",
		}),
		"metav1GetOptions": c.Universe.Function(types.Name{
			Package: "k8s.io/apimachinery/pkg/apis/meta/v1",
			Name:    "GetOptions",
		}),
		"zapSugaredLogger": c.Universe.Type(types.Name{
			Package: "go.uber.org/zap",
			Name:    "SugaredLogger",
		}),
	}

	sw.Do(reconcilerInterfaceFactory, m)
	sw.Do(reconcilerNewReconciler, m)
	sw.Do(reconcilerImplFactory, m)
	sw.Do(reconcilerStatusFactory, m)
	sw.Do(reconcilerFinalizerFactory, m)

	return sw.Error()
}

var reconcilerInterfaceFactory = `
// Interface defines the strongly typed interfaces to be implemented by a
// controller reconciling {{.type|raw}}.
type Interface interface {
	// ReconcileKind implements custom logic to reconcile {{.type|raw}}. Any changes
	// to the objects .Status or .Finalizers will be propagated to the stored
	// object. It is recommended that implementors do not call any update calls
	// for the Kind inside of ReconcileKind, it is the responsibility of the calling
	// controller to propagate those properties. The resource passed to ReconcileKind
 	// will always have an empty deletion timestamp.
	ReconcileKind(ctx context.Context, o *{{.type|raw}}) {{.reconcilerEvent|raw}}
}

// Finalizer defines the strongly typed interfaces to be implemented by a
// controller finalizing {{.type|raw}}.
type Finalizer interface {
	// FinalizeKind implements custom logic to finalize {{.type|raw}}. Any changes
	// to the objects .Status or .Finalizers will be ignored. Returning a nil or
	// Normal type {{.reconcilerEvent|raw}} will allow the finalizer to be deleted on
	// the resource. The resource passed to FinalizeKind will always have a set
	// deletion timestamp.
	FinalizeKind(ctx context.Context, o *{{.type|raw}}) {{.reconcilerEvent|raw}}
}

// reconcilerImpl implements controller.Reconciler for {{.type|raw}} resources.
type reconcilerImpl struct {
	// Client is used to write back status updates.
	Client {{.clientsetInterface|raw}}

	// Listers index properties about resources
	Lister {{.resourceLister|raw}}

	// Recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	Recorder {{.recordEventRecorder|raw}}

	// reconciler is the implementation of the business logic of the resource.
	reconciler Interface
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*reconcilerImpl)(nil)

`

var reconcilerNewReconciler = `
func NewReconciler(ctx context.Context, logger *{{.zapSugaredLogger|raw}}, client {{.clientsetInterface|raw}}, lister {{.resourceLister|raw}}, recorder {{.recordEventRecorder|raw}}, r Interface) {{.controllerReconciler|raw}} {
	return &reconcilerImpl{
		Client: client,
		Lister: lister,
		Recorder: recorder,
		reconciler:    r,
	}
}
`

var reconcilerImplFactory = `
// Reconcile implements controller.Reconciler
func (r *reconcilerImpl) Reconcile(ctx context.Context, key string) error {
	logger := {{.loggingFromContext|raw}}(ctx)

	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := {{.cacheSplitMetaNamespaceKey|raw}}(key)
	if err != nil {
		logger.Errorf("invalid resource key: %s", key)
		return nil
	}

    // TODO(n3wscott): this is needed for serving.
 	// If our controller has configuration state, we'd "freeze" it and
	// attach the frozen configuration to the context.
	//    ctx = r.configStore.ToContext(ctx)

	// Get the resource with this namespace/name.
	original, err := r.Lister.{{.type|apiGroup}}(namespace).Get(name)
	if {{.apierrsIsNotFound|raw}}(err) {
		// The resource may no longer exist, in which case we stop processing.
		logger.Errorf("resource %q no longer exists", key)
		return nil
	} else if err != nil {
		return err
	}
	// Don't modify the informers copy.
	resource := original.DeepCopy()

	var reconcileEvent {{.reconcilerEvent|raw}}
	if resource.GetDeletionTimestamp().IsZero() {
		// Set and update the finalizer on resource if r.reconciler
		// implements Finalizer.
		if err := r.setFinalizerIfFinalizer(ctx, resource); err != nil {
			logger.Warnw("Failed to set finalizers", zap.Error(err))
		}

		// Reconcile this copy of the resource and then write back any status
		// updates regardless of whether the reconciliation errored out.
		reconcileEvent = r.reconciler.ReconcileKind(ctx, resource)
	} else if fin, ok := r.reconciler.(Finalizer); ok {
		// For finalizing reconcilers, if this resource being marked for deletion
		// and reconciled cleanly (nil or normal event), remove the finalizer.
		reconcileEvent = fin.FinalizeKind(ctx, resource)
		if err := r.clearFinalizer(ctx, resource, reconcileEvent); err != nil {
			logger.Warnw("Failed to clear finalizers", zap.Error(err))
		}
	}

	// Synchronize the status.
	if equality.Semantic.DeepEqual(original.Status, resource.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the injectionInformer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if err = r.updateStatus(original, resource); err != nil {
		logger.Warnw("Failed to update resource status", zap.Error(err))
		r.Recorder.Eventf(resource, {{.corev1EventTypeWarning|raw}}, "UpdateFailed",
			"Failed to update status for %q: %v", resource.Name, err)
		return err
	}

	// Report the reconciler event, if any.
	if reconcileEvent != nil {
		var event *{{.reconcilerReconcilerEvent|raw}}
		if reconciler.EventAs(reconcileEvent, &event) {
			logger.Infow("ReconcileKind returned an event", zap.Any("event", reconcileEvent))
			r.Recorder.Eventf(resource, event.EventType, event.Reason, event.Format, event.Args...)
			return nil
		} else {
			logger.Errorw("ReconcileKind returned an error", zap.Error(reconcileEvent))
			r.Recorder.Event(resource, {{.corev1EventTypeWarning|raw}}, "InternalError", reconcileEvent.Error())
			return reconcileEvent
		}
	}
	return nil
}
`

var reconcilerStatusFactory = `
func (r *reconcilerImpl) updateStatus(existing *{{.type|raw}}, desired *{{.type|raw}}) error {
	existing = existing.DeepCopy()
	return {{.reconcilerRetryUpdateConflicts|raw}}(func(attempts int) (err error) {
		// The first iteration tries to use the injectionInformer's state, subsequent attempts fetch the latest state via API.
		if attempts > 0 {
			existing, err = r.Client.{{.type|versionedClientset}}().{{.type|apiGroup}}(desired.Namespace).Get(desired.Name, {{.metav1GetOptions|raw}}{})
			if err != nil {
				return err
			}
		}

		// If there's nothing to update, just return.
		if reflect.DeepEqual(existing.Status, desired.Status) {
			return nil
		}

		existing.Status = desired.Status
		_, err = r.Client.{{.type|versionedClientset}}().{{.type|apiGroup}}(existing.Namespace).UpdateStatus(existing)
		return err
	})
}
`

var reconcilerFinalizerFactory = `
// updateFinalizersFiltered will update the Finalizers of the resource.
// TODO: this method could be generic and sync all finalizers. For now it only
// updates defaultFinalizerName.
func (r *reconcilerImpl) updateFinalizersFiltered(ctx context.Context, resource *{{.type|raw}}) error {
	finalizerName := defaultFinalizerName

	actual, err := r.Lister.{{.type|apiGroup}}(resource.Namespace).Get(resource.Name)
	if err != nil {
		return err
	}

	// Don't modify the informers copy.
	existing := actual.DeepCopy()

	var finalizers []string

	// If there's nothing to update, just return.
	existingFinalizers := sets.NewString(existing.Finalizers...)
	desiredFinalizers := sets.NewString(resource.Finalizers...)

	if desiredFinalizers.Has(finalizerName) {
		if existingFinalizers.Has(finalizerName) {
			// Nothing to do.
			return nil
		}
		// Add the finalizer.
		finalizers = append(existing.Finalizers, finalizerName)
	} else {
		if !existingFinalizers.Has(finalizerName) {
			// Nothing to do.
			return nil
		}
		// Remove the finalizer.
		existingFinalizers.Delete(finalizerName)
		finalizers = existingFinalizers.List()
	}

	mergePatch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"finalizers":      finalizers,
			"resourceVersion": existing.ResourceVersion,
		},
	}

	patch, err := json.Marshal(mergePatch)
	if err != nil {
		return err
	}

	_, err = r.Client.{{.type|versionedClientset}}().{{.type|apiGroup}}(resource.Namespace).Patch(resource.Name, types.MergePatchType, patch)
	if err != nil {
		r.Recorder.Eventf(resource, {{.corev1EventTypeWarning|raw}}, "FinalizerUpdateFailed",
			"Failed to update finalizers for %q: %v", resource.Name, err)
	} else {
		r.Recorder.Eventf(resource, {{.corev1EventTypeNormal|raw}}, "FinalizerUpdate",
			"Updated %q finalizers", resource.GetName())
	}
	return err
}

func (r *reconcilerImpl) setFinalizerIfFinalizer(ctx context.Context, resource *{{.type|raw}}) error {
	if _, ok := r.reconciler.(Finalizer); !ok {
		return nil
	}

	finalizers := sets.NewString(resource.Finalizers...)

	// If this resource is not being deleted, mark the finalizer.
	if resource.GetDeletionTimestamp().IsZero() {
		finalizers.Insert(defaultFinalizerName)
	}

	resource.Finalizers = finalizers.List()

	// Synchronize the finalizers filtered by defaultFinalizerName.
	return r.updateFinalizersFiltered(ctx, resource)
}

func (r *reconcilerImpl) clearFinalizer(ctx context.Context, resource *{{.type|raw}}, reconcileEvent {{.reconcilerEvent|raw}}) error {
	if _, ok := r.reconciler.(Finalizer); !ok {
		return nil
	}
	if resource.GetDeletionTimestamp().IsZero() {
		return nil
	}

	finalizers := sets.NewString(resource.Finalizers...)

	if reconcileEvent != nil {
		var event *{{.reconcilerReconcilerEvent|raw}}
		if reconciler.EventAs(reconcileEvent, &event) {
			if event.EventType == v1.EventTypeNormal {
				finalizers.Delete(defaultFinalizerName)
			}
		}
	} else {
		finalizers.Delete(defaultFinalizerName)
	}

	resource.Finalizers = finalizers.List()

	// Synchronize the finalizers filtered by defaultFinalizerName.
	return r.updateFinalizersFiltered(ctx, resource)
}
`
