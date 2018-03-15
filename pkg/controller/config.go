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

type Config struct {
	DomainSuffix string `json:domainSuffix`
}

type ConfigHolder interface {
	GetConfig() Config
}

type YamlConfigHolder struct {
	config Config
}

func (h *YamlConfigHolder) GetConfig() Config {
	return h.config
}

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

// TODO(nghia)  Improve this ConfigHolder implementation so that
//              it watches for changes in the config file for updates.
func NewConfigHolder(filename string) (ConfigHolder, error) {
	h := YamlConfigHolder{}
	if err := h.Load(filename); err != nil {
		return nil, err
	}
	return &h, nil
}
