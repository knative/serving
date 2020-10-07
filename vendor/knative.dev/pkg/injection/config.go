/*
Copyright 2020 The Knative Authors

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
	"errors"
	"flag"
	"log"
	"os"
	"os/user"
	"path/filepath"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
)

// ParseAndGetRESTConfigOrDie parses the rest config flags and creates a client or
// dies by calling log.Fatalf.
func ParseAndGetRESTConfigOrDie() *rest.Config {
	var (
		serverURL = flag.String("server", "",
			"The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
		kubeconfig = flag.String("kubeconfig", "",
			"Path to a kubeconfig. Only required if out-of-cluster.")
	)
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	cfg, err := GetRESTConfig(*serverURL, *kubeconfig)
	if err != nil {
		log.Fatal("Error building kubeconfig: ", err)
	}

	return cfg
}

// GetRESTConfig returns a rest.Config to be used for kubernetes client creation.
// It does so in the following order:
//   1. Use the passed kubeconfig/serverURL.
//   2. Fallback to the KUBECONFIG environment variable.
//   3. Fallback to in-cluster config.
//   4. Fallback to the ~/.kube/config.
func GetRESTConfig(serverURL, kubeconfig string) (*rest.Config, error) {
	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}

	// If we have an explicit indication of where the kubernetes config lives, read that.
	if kubeconfig != "" {
		c, err := clientcmd.BuildConfigFromFlags(serverURL, kubeconfig)
		if err != nil {
			return nil, err
		}
		return c, nil
	}

	// If not, try the in-cluster config.
	if c, err := rest.InClusterConfig(); err == nil {
		return c, nil
	}

	// If no in-cluster config, try the default location in the user's home directory.
	if usr, err := user.Current(); err == nil {
		if c, err := clientcmd.BuildConfigFromFlags("", filepath.Join(usr.HomeDir, ".kube", "config")); err == nil {
			return c, nil
		}
	}

	return nil, errors.New("could not create a valid kubeconfig")
}
