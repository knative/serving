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
package environment

import (
	"bytes"
	"flag"
	"os"
	"text/template"
)

type Images struct {
	Template   string // Template to build the image reference (defaults to {{.Repository}}/{{.Name}}:{{.Tag}})
	Repository string // Image repository (defaults to $KO_DOCKER_REPO)
	Tag        string // Tag for test images
}

func (i *Images) AddFlags(fs *flag.FlagSet) {
	defaultRepo := os.Getenv("KO_DOCKER_REPO")
	fs.StringVar(&i.Repository, "env.image.repo", defaultRepo,
		"Provide the uri of the docker repo you have uploaded the test image to using `uploadtestimage.sh`. Defaults to $KO_DOCKER_REPO")

	fs.StringVar(&i.Template, "env.image.template", "{{.Repository}}/{{.Name}}:{{.Tag}}",
		"Provide a template to generate the reference to an image from the test. Defaults to `{{.Repository}}/{{.Name}}:{{.Tag}}`.")

	fs.StringVar(&i.Tag, "env.image.tag", "latest", "Provide the version tag for the test images.")
}

func (i *Images) Path(name string) string {
	tpl, err := template.New("image").Parse(i.Template)
	if err != nil {
		panic("could not parse image template: " + err.Error())
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, struct {
		Repository string
		Name       string
		Tag        string
	}{
		Repository: i.Repository,
		Name:       name,
		Tag:        i.Tag,
	}); err != nil {
		panic("could not apply the image template: " + err.Error())
	}
	return buf.String()
}
