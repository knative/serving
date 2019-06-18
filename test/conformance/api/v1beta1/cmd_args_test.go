// +build e2e

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

package v1beta1

import (
	"fmt"
	"testing"

	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	"github.com/knative/serving/test"
	v1b1test "github.com/knative/serving/test/v1beta1"
)

func TestCmdArgsService(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "python:3",
	}

	// Clean up on test failure or interrupt
	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	const text = "THIS IS THE CMD AND ARGS TEST"

	// Setup initial Service
	_, err := v1b1test.CreateServiceReady(t, clients, &names,
		func(svc *v1beta1.Service) {
			c := &svc.Spec.Template.Spec.Containers[0]
			c.Image = names.Image
			c.Command = []string{"python"}
			c.Args = []string{"-c", fmt.Sprintf(`
import http.server
import socketserver
from http import HTTPStatus


class Handler(http.server.SimpleHTTPRequestHandler):
    def do_GET(self):
        self.send_response(HTTPStatus.OK)
        self.end_headers()
        self.wfile.write(b'%s')


httpd = socketserver.TCPServer(('', 8080), Handler)
httpd.serve_forever()`, text),
			}
		})
	if err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	if err = validateDataPlane(t, clients, names, text); err != nil {
		t.Error(err)
	}
}
