/*
Copyright 2017 The Kubernetes Authors.

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

package route

import (
	"github.com/google/elafros/pkg/apis/ela/v1alpha1"
	elatyped "github.com/google/elafros/pkg/client/clientset/versioned/typed/ela/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConfigurationCache is a set caching configurations from ela configuraiton
// client. If a configuration is not in it, it will fetch via client.
type ConfigurationCache struct {
	// The client used to fetch configurations.
	configClient elatyped.ConfigurationInterface
	// The cache for configuratiosn.
	configSet map[string]*v1alpha1.Configuration
}

// Get returns a configuration with the given name. If the target configuration
// is not in the cache, it fetches via client.
func (configCache *ConfigurationCache) Get(name string) (*v1alpha1.Configuration, error) {
	if config, ok := configCache.configSet[name]; ok {
		return config, nil
	}

	config, err := configCache.configClient.Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	// Update the cache
	configCache.configSet[name] = config
	return config, nil
}

// List return a list of configurations which have been cached in this cache.
func (configCache *ConfigurationCache) List() []v1alpha1.Configuration {
	ret := []v1alpha1.Configuration{}
	for _, config := range configCache.configSet {
		ret = append(ret, *config)
	}
	return ret
}
