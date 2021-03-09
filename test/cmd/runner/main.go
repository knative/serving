/*
Copyright 2021 The Knative Authors

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
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const resourcesEnvVar = "KNATIVE_RESOURCES_PATH"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) (exitCode int) {
	var (
		err       error
		resources []*resourceInfo
		state     *os.ProcessState
		ctx       context.Context = setupSignals()
	)

	path := os.Getenv(resourcesEnvVar)
	if path == "" {
		path = "testdata/env"
	}

	if resources, err = loadFixtures(path); err != nil {
		log("failed to load test resources %s", err)
		exitCode = 1
		return
	}

	// Defer so we can cleanup properly if a panic occurs
	// when we run the test binary
	defer func() {
		// don't use the prior context since it could have been
		// cancelled with an interrupt signal
		err = revertResources(context.Background(), resources)
		if err != nil {
			exitCode = 1
		}
	}()

	err = applyResources(ctx, resources)
	if err != nil {
		exitCode = 1
		return
	}

	state, err = runTestBinary(ctx, args)
	if err != nil {
		log("failed to run test binary %s", err)
		exitCode = 1
		return
	}

	exitCode = state.ExitCode()
	return
}

func applyResources(ctx context.Context, resources []*resourceInfo) error {
	for _, r := range resources {
		action := "applying"

		if r.isPatch() {
			action = "patching"
		}

		log("%s %s", action, r)
		if err := r.apply(ctx); err != nil {
			log("error %s %s: %s", action, r, err)
			return err
		}
		r.applied = true
	}
	return nil
}

func revertResources(ctx context.Context, resources []*resourceInfo) error {
	var lastErr error
	for _, resource := range resources {
		if !resource.applied {
			continue
		}
		log("reverting %s", resource)
		err := resource.revert(ctx)
		if err != nil {
			lastErr = err
			log("error reverting %s %s", resource, err)
		}
	}
	return lastErr
}

func log(format string, msg ...interface{}) {
	fmt.Printf("[runner] ")
	fmt.Printf(format, msg...)
	fmt.Println()
}

func runTestBinary(ctx context.Context, args []string) (*os.ProcessState, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()

	var exerr *exec.ExitError
	if errors.As(err, &exerr) {
		return exerr.ProcessState, nil
	}
	return cmd.ProcessState, err
}
