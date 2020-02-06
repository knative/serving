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

package boskos

import (
	"context"
	"fmt"
	"time"

	"knative.dev/pkg/testutils/clustermanager/e2e-tests/common"

	boskosclient "k8s.io/test-infra/boskos/client"
	boskoscommon "k8s.io/test-infra/boskos/common"
)

const (
	// GKEProjectResource is resource type defined for GKE projects
	GKEProjectResource = "gke-project"
)

var (
	boskosURI           = "http://boskos.test-pods.svc.cluster.local."
	defaultWaitDuration = time.Minute * 20
)

// Operation defines actions for handling GKE resources
type Operation interface {
	AcquireGKEProject(string) (*boskoscommon.Resource, error)
	ReleaseGKEProject(string) error
}

// Client a wrapper around k8s boskos client that implements Operation
type Client struct {
	*boskosclient.Client
}

// NewClient creates a boskos Client with GKE operation. The owner of any resources acquired
// by this client is the same as the host name. `user` and `pass` are used for basic
// authentication for boskos client where pass is a password file. `user` and `pass` fields
// are passed directly to k8s boskos client. Refer to
// [k8s boskos](https://github.com/kubernetes/test-infra/tree/master/boskos) for more details.
// If host is "", it looks up JOB_NAME environment variable and set it to be the host name.
func NewClient(host string, user string, pass string) (*Client, error) {
	if host == "" {
		host = common.GetOSEnv("JOB_NAME")
	}

	c, err := boskosclient.NewClient(host, boskosURI, user, pass)
	if err != nil {
		return nil, err
	}

	return &Client{c}, nil
}

// AcquireGKEProject acquires GKE Boskos Project with "free" state, and not
// owned by anyone, sets its state to "busy" and assign it an owner of *host,
// which by default is env var `JOB_NAME`.
func (c *Client) AcquireGKEProject(resType string) (*boskoscommon.Resource, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultWaitDuration)
	defer cancel()
	p, err := c.AcquireWait(ctx, resType, boskoscommon.Free, boskoscommon.Busy)
	if err != nil {
		return nil, fmt.Errorf("boskos failed to acquire GKE project: %v", err)
	}
	if p == nil {
		return nil, fmt.Errorf("boskos does not have a free %s at the moment", resType)
	}
	return p, nil
}

// ReleaseGKEProject releases project, the host must match with the host name that acquired
// the project, which by default is env var `JOB_NAME`. The state is set to
// "dirty" for Janitor picking up.
// This function is very powerful, it can release Boskos resource acquired by
// other processes, regardless of where the other process is running.
func (c *Client) ReleaseGKEProject(name string) error {
	if err := c.Release(name, boskoscommon.Dirty); err != nil {
		return fmt.Errorf("boskos failed to release GKE project '%s': %v", name, err)
	}
	return nil
}
