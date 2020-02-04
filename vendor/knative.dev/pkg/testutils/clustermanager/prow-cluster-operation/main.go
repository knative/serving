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

package main

import (
	"errors"
	"flag"
	"log"

	"knative.dev/pkg/testutils/clustermanager/prow-cluster-operation/actions"
	"knative.dev/pkg/testutils/clustermanager/prow-cluster-operation/options"
)

var (
	create bool
	delete bool
	get    bool
)

func main() {
	var err error

	flag.BoolVar(&create, "create", false, "Create cluster")
	flag.BoolVar(&delete, "delete", false, "Delete cluster")
	flag.BoolVar(&get, "get", false, "Get existing cluster from kubeconfig or gcloud")
	o := options.NewRequestWrapper()
	flag.Parse()

	if (create && delete) || (create && get) || (delete && get) {
		log.Fatal("--create, --delete, --get are mutually exclusive")
	}
	switch {
	case create:
		_, err = actions.Create(o)
	case delete:
		err = actions.Delete(o)
	case get:
		_, err = actions.Get(o)
	default:
		err = errors.New("must pass one of --create, --delete, --get")
	}

	if err != nil {
		log.Fatal(err)
	}
}
