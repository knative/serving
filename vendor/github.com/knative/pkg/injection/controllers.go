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

package injection

import (
	"context"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
)

// ControllerInjector holds the type of a callback that attaches a particular
// controller type to a context.
type ControllerInjector func(context.Context, configmap.Watcher) *controller.Impl

func (i *impl) RegisterController(ii ControllerInjector) {
	i.m.Lock()
	defer i.m.Unlock()

	i.controllers = append(i.controllers, ii)
}

func (i *impl) GetControllers() []ControllerInjector {
	i.m.RLock()
	defer i.m.RUnlock()

	// Copy the slice before returning.
	return append(i.controllers[:0:0], i.controllers...)
}
