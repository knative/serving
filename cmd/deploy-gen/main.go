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

package main

import (
	"io/ioutil"
	"log"
	"os"
	"text/template"

	"github.com/ghodss/yaml"
	"k8s.io/apimachinery/pkg/api/resource"
)

func main() {
	var p Params

	b, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalf("ioutil.ReadAll() = %v", err)
	}

	if err := yaml.Unmarshal(b, &p); err != nil {
		log.Fatalf("yaml.Unmarshal() = %v", err)
	}

	t, err := template.New("deployment").Funcs(template.FuncMap{
		"r2s": func(rq resource.Quantity) string {
			return rq.String()
		},
	}).Parse(tmpl)
	if err != nil {
		log.Fatalf("template.Parse() = %v", err)
	}

	if err := t.Execute(os.Stdout, p); err != nil {
		log.Fatalf("t.Execute() = %v", err)
	}
}
