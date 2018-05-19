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
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/google/go-containerregistry/ko/build"
	"github.com/google/go-containerregistry/ko/publish"
	"github.com/google/go-containerregistry/ko/resolve"
	"github.com/google/go-containerregistry/name"
	"github.com/google/go-containerregistry/v1/remote"
)

func gobuildOptions() build.Options {
	return build.Options{
		GetBase: GetBaseImage,
	}
}

func resolveFilesToWriter(fo *FilenameOptions, out io.Writer) {
	fs, err := enumerateFiles(fo)
	if err != nil {
		log.Fatalf("error enumerating files: %v", err)
	}

	opt := gobuildOptions()
	var sm sync.Map
	wg := sync.WaitGroup{}
	for _, f := range fs {
		wg.Add(1)
		go func(f string) {
			defer wg.Done()

			b, err := resolveFile(f, opt)
			if err != nil {
				log.Fatalf("error processing import paths in %q: %v", f, err)
			}
			sm.Store(f, b)
		}(f)
	}
	// Wait for all of the go routines to complete.
	wg.Wait()
	for _, f := range fs {
		iface, ok := sm.Load(f)
		if !ok {
			log.Fatalf("missing file in resolved map: %v", f)
		}
		b, ok := iface.([]byte)
		if !ok {
			log.Fatalf("unsupported type in sync.Map's value: %T", iface)
		}
		// Our sole output should be the resolved yamls
		out.Write([]byte("---\n"))
		out.Write(b)
	}
}

func resolveFile(f string, opt build.Options) ([]byte, error) {
	repoName := os.Getenv("KO_DOCKER_REPO")
	repo, err := name.NewRepository(repoName, name.WeakValidation)
	if err != nil {
		return nil, fmt.Errorf("the environment variable KO_DOCKER_REPO must be set to a valid docker repository, got %v", err)
	}

	b, err := ioutil.ReadFile(f)
	if err != nil {
		return nil, err
	}

	publisher := publish.NewDefault(repo, http.DefaultTransport, remote.WriteOptions{
		MountPaths: GetMountPaths(),
	})
	builder, err := build.NewGo(opt)
	if err != nil {
		return nil, err
	}

	// TODO(mattmoor): To better approximate Bazel, we should collect the importpath references
	// in advance, trigger builds, and then do a second pass to finalize each of the configs.
	return resolve.ImageReferences(b, builder, publisher)
}
