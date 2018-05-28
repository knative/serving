// Copyright 2018 Google LLC All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"log"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

// runCmd is suitable for use with cobra.Command's Run field.
type runCmd func(*cobra.Command, []string)

// passthru returns a runCmd that simply passes our CLI arguments
// through to a binary named command.
func passthru(command string) runCmd {
	return func(_ *cobra.Command, _ []string) {
		// Start building a command line invocation by passing
		// through our arguments to command's CLI.
		cmd := exec.Command(command, os.Args[1:]...)

		// Pass through our environment
		cmd.Env = os.Environ()
		// Pass through our stdfoo
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		cmd.Stdin = os.Stdin

		// Run it.
		if err := cmd.Run(); err != nil {
			log.Fatalf("error executing %q command with args: %v; %v", command, os.Args[1:], err)
		}
	}
}

// addKubeCommands augments our CLI surface with a passthru delete command, and an apply
// command that realizes the promise of ko, as outlined here:
//    https://github.com/google/go-containerregistry/issues/80
func addKubeCommands(topLevel *cobra.Command) {
	topLevel.AddCommand(&cobra.Command{
		Use:   "delete",
		Short: `See "kubectl help delete" for detailed usage.`,
		Run:   passthru("kubectl"),
		// We ignore unknown flags to avoid importing everything Go exposes
		// from our commands.
		FParseErrWhitelist: cobra.FParseErrWhitelist{
			UnknownFlags: true,
		},
	})

	lo := &LocalOptions{}
	fo := &FilenameOptions{}
	apply := &cobra.Command{
		Use:   "apply -f FILENAME",
		Short: "Apply the input files with image references resolved to built/pushed image digests.",
		Long:  `This sub-command finds import path references within the provided files, builds them into Go binaries, containerizes them, publishes them, and then feeds the resulting yaml into "kubectl apply".`,
		Example: `
  # Build and publish import path references to a Docker
  # Registry as:
  #   ${KO_DOCKER_REPO}/<import path>
  # Then, feed the resulting yaml into "kubectl apply"
  ko apply -f config/

  # Build and publish import path references to a Docker
  # daemon as:
  #   ko.local/<import path>
  # Then, feed the resulting yaml into "kubectl apply"
  ko apply -L -f config/`,
		Run: func(cmd *cobra.Command, args []string) {
			// TODO(mattmoor): Use io.Pipe to avoid buffering the whole thing.
			buf := bytes.NewBuffer(nil)
			resolveFilesToWriter(fo, lo, buf)

			// Issue a "kubectl apply" command reading from stdin,
			// to which we will pipe the resolved files.
			kubectlCmd := exec.Command("kubectl", "apply", "-f", "-")

			// Pass through our environment
			kubectlCmd.Env = os.Environ()
			// Pass through our std{out,err} and make our resolved buffer stdin.
			kubectlCmd.Stderr = os.Stderr
			kubectlCmd.Stdout = os.Stdout
			kubectlCmd.Stdin = buf

			// Run it.
			if err := kubectlCmd.Run(); err != nil {
				log.Fatalf("error executing \"kubectl apply\": %v", err)
			}
		},
	}
	addLocalArg(apply, lo)
	addFileArg(apply, fo)
	topLevel.AddCommand(apply)

	resolve := &cobra.Command{
		Use:   "resolve -f FILENAME",
		Short: "Print the input files with image references resolved to built/pushed image digests.",
		Long:  `This sub-command finds import path references within the provided files, builds them into Go binaries, containerizes them, publishes them, and prints the resulting yaml.`,
		Example: `
  # Build and publish import path references to a Docker
  # Registry as:
  #   ${KO_DOCKER_REPO}/<import path>
  ko resolve -f config/

  # Build and publish import path references to a Docker
  # daemon as:
  #   ko.local/<import path>
  ko resolve -L -f config/`,
		Run: func(cmd *cobra.Command, args []string) {
			resolveFilesToWriter(fo, lo, os.Stdout)
		},
	}
	addLocalArg(resolve, lo)
	addFileArg(resolve, fo)
	topLevel.AddCommand(resolve)

	publish := &cobra.Command{
		Use:   "publish IMPORTPATH...",
		Short: "Build and publish container images from the given importpaths.",
		Long:  `This sub-command builds the provided import paths into Go binaries, containerizes them, and publishes them.`,
		Example: `
  # Build and publish import path references to a Docker
  # Registry as:
  #   ${KO_DOCKER_REPO}/<import path>
  ko publish github.com/foo/bar/cmd/baz github.com/foo/bar/cmd/blah

  # Build and publish import path references to a Docker
  # daemon as:
  #   ko.local/<import path>
  ko publish -L github.com/foo/bar/cmd/baz github.com/foo/bar/cmd/blah`,
		Args: cobra.MinimumNArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			publishImages(args, lo)
		},
	}
	addLocalArg(publish, lo)
	topLevel.AddCommand(publish)
}
