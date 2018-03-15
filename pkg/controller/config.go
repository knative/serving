/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"io/ioutil"

	"github.com/ghodss/yaml"
)

// Contains controller configurations.
type Config struct {

	// The suffix of the domain used for routes.
	DomainSuffix string `json:"domainSuffix"`
}

type ConfigHolder interface {

	// Returns the current value of Config.  It is expected that
	// subsequent calls may change the value, so clients should be
	// careful when holding the value.
	GetConfig() Config
}

// An implementation of ConfigHolder based on a YAML file.  In this
// implementation we load the file once and does not check again when
// its content changes.
//
// TODO(nghia)  Improve this ConfigHolder implementation so that
//              it watches for changes in the config file for updates,
//              may be using something like github.com/fsnotify/fsnotify.
type YamlConfigHolder struct {
	config Config
}

func (h *YamlConfigHolder) GetConfig() Config {
	return h.config
}

// Load the configuration from a YAML file.
func (h *YamlConfigHolder) Load(filename string) error {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}
	if err = yaml.Unmarshal(content, &h.config); err != nil {
		return err
	}
	return nil
}

// Load a YAML file and returns a ConfigHolder using the parsed Config.
// This loads once and does not update with file updates.
func NewConfigHolder(filename string) (ConfigHolder, error) {
	h := YamlConfigHolder{}
	if err := h.Load(filename); err != nil {
		return nil, err
	}
	return &h, nil
}
