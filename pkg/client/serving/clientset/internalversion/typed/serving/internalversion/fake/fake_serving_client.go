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

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	rest "k8s.io/client-go/rest"
	testing "k8s.io/client-go/testing"
	internalversion "knative.dev/serving/pkg/client/serving/clientset/internalversion/typed/serving/internalversion"
)

type FakeServing struct {
	*testing.Fake
}

func (c *FakeServing) Configurations(namespace string) internalversion.ConfigurationInterface {
	return &FakeConfigurations{c, namespace}
}

func (c *FakeServing) Revisions(namespace string) internalversion.RevisionInterface {
	return &FakeRevisions{c, namespace}
}

func (c *FakeServing) Routes(namespace string) internalversion.RouteInterface {
	return &FakeRoutes{c, namespace}
}

func (c *FakeServing) Services(namespace string) internalversion.ServiceInterface {
	return &FakeServices{c, namespace}
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *FakeServing) RESTClient() rest.Interface {
	var ret *rest.RESTClient
	return ret
}
