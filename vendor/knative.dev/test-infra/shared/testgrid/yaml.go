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

package testgrid

import (
	"fmt"
	"io/ioutil"
	"path"

	"gopkg.in/yaml.v2"

	"knative.dev/test-infra/shared/common"
)

const configPath = "config/prow/testgrid/testgrid.yaml"

// Config is entire testgrid config
type Config struct {
	Dashboards []Dashboard `yaml:"dashboards"`
}

// Dashboard is single dashboard on testgrid
type Dashboard struct {
	Name string `yaml:"name"`
	Tabs []Tab  `yaml:"dashboard_tab"`
}

// Tab is a single tab on testgrid
type Tab struct {
	Name          string `yaml:"name"`
	TestGroupName string `yaml:"test_group_name"`
}

// NewConfig loads from default config
func NewConfig() (*Config, error) {
	root, err := common.GetRootDir()
	if err != nil {
		return nil, err
	}
	return NewConfigFromFile(path.Join(root, configPath))
}

// NewConfigFromFile loads config from file
func NewConfigFromFile(fp string) (*Config, error) {
	ac := &Config{}
	contents, err := ioutil.ReadFile(fp)
	if err == nil {
		err = yaml.Unmarshal(contents, ac)
	}
	if err != nil {
		return nil, err
	}
	return ac, err
}

// GetTabRelURL finds URL relative to testgrid home URL from testgroup name
// (generally this is prow job name)
func (ac *Config) GetTabRelURL(tgName string) (string, error) {
	for _, dashboard := range ac.Dashboards {
		for _, tab := range dashboard.Tabs {
			if tab.TestGroupName == tgName {
				return fmt.Sprintf("%s#%s", dashboard.Name, tab.Name), nil
			}
		}
	}
	return "", fmt.Errorf("testgroup name '%s' not exist", tgName)
}
