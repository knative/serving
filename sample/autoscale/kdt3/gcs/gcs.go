/*
Copyright 2018 Google LLC

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
package gcs

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"

	m "github.com/elafros/elafros/sample/autoscale/kdt3/model"
)

var resourcesUrl string

func init() {
	resourcesUrl = os.Getenv("RESOURCES_URL")
}

func Load(gameId string) (*m.Game, error) {
	g := &m.Game{}
	var raw []byte
	if resourcesUrl == "" {
		r, err := ioutil.ReadFile("resources/game.json")
		if err != nil {
			return nil, err
		}
		raw = r
	} else {
		res, err := http.Get(resourcesUrl + "game.json")
		if err != nil {
			return nil, err
		}
		r, err := ioutil.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			return nil, err
		}
		raw = r
	}
	err := json.Unmarshal(raw, g)
	if err != nil {
		return nil, err
	}
	return g, nil
}
