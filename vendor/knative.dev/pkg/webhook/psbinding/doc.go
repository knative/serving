/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless requ ired by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package psbinding provides facilities to make authoring Bindings that
// work with "Pod Spec"-able subjects easier.  There are two key components
//  1. The AdmissionController, which lives in psbinding.go (controller.go)
//    sets it up.
//  2. The BaseReconciler, which lives in reconciler.go and can either be
//    used directly as a Reconciler, or it can be wrapped as a base
//    implementation for a customized reconciler that wants to take advantage
//    of its facilities (e.g. for updating status and manipulating finalizers).
//
// The core concept to consuming psbinding is the Bindable interface.  By
// implementing Bindable on your binding resource, you enable the BaseReconciler
// to take over a significant amount of the boilerplate reconciliation (maybe
// all of it).  A lot of the interface methods will seem pretty standard to
// Knative folks, but the two key methods to call our are Do and Undo.  These
// "mutation" methods carry the business logic for the "Pod Spec"-able binding.
//
// The mutation methods have the signature:
//    func(context.Context, *v1alpha1.WithPod)
// These methods are called to have the Binding perform its mutation on the
// supplied WithPod (our "Pod Spec"-able wrapper type).  However, in some
// cases the binding may need additional context.  Similar to apis.Validatable
// and apis.Defaultable these mutations take a context.Context, and similar
// to our "resourcesemantics" webhook, the "psbinding" package provides a hook
// to allow consumers to infuse this context.Context with additional... context.
// The signature of these hooks is BindableContext, and they may be supplied to
// both the AdmissionController and the BaseReconciler.
package psbinding
