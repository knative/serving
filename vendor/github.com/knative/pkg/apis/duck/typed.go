/*
Copyright 2018 The Knative Authors

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

package duck

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"

	"github.com/knative/pkg/apis"
)

// TypedInformerFactory implements InformerFactory such that the elements
// tracked by the informer/lister have the type of the canonical "obj".
type TypedInformerFactory struct {
	Client       dynamic.Interface
	Type         apis.Listable
	ResyncPeriod time.Duration
	StopChannel  <-chan struct{}
}

// Check that TypedInformerFactory implements InformerFactory.
var _ InformerFactory = (*TypedInformerFactory)(nil)

// Get implements InformerFactory.
func (dif *TypedInformerFactory) Get(gvr schema.GroupVersionResource) (cache.SharedIndexInformer, cache.GenericLister, error) {
	listObj := dif.Type.GetListType()
	lw := &cache.ListWatch{
		ListFunc:  asStructuredLister(dif.Client.Resource(gvr).List, listObj),
		WatchFunc: dif.Client.Resource(gvr).Watch,
	}
	inf := cache.NewSharedIndexInformer(lw, dif.Type, dif.ResyncPeriod, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})

	lister := cache.NewGenericLister(inf.GetIndexer(), gvr.GroupResource())

	go inf.Run(dif.StopChannel)

	if ok := cache.WaitForCacheSync(dif.StopChannel, inf.HasSynced); !ok {
		return nil, nil, fmt.Errorf("Failed starting shared index informer for %v with type %T", gvr, dif.Type)
	}

	return inf, lister, nil
}

type unstructuredLister func(metav1.ListOptions) (*unstructured.UnstructuredList, error)

func asStructuredLister(ulist unstructuredLister, listObj runtime.Object) cache.ListFunc {
	return func(opts metav1.ListOptions) (runtime.Object, error) {
		ul, err := ulist(opts)
		if err != nil {
			return nil, err
		}
		res := listObj.DeepCopyObject()
		if err := FromUnstructured(ul, res); err != nil {
			return nil, err
		}
		return res, nil
	}
}
