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
	"os"
	"sync"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/ko/build"
	"github.com/google/go-containerregistry/pkg/ko/publish"
	"github.com/google/go-containerregistry/pkg/ko/resolve"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
)

func gobuildOptions() ([]build.Option, error) {
	creationTime, err := getCreationTime()
	if err != nil {
		return nil, err
	}
	opts := []build.Option{
		build.WithBaseImages(getBaseImage),
	}
	if creationTime != nil {
		opts = append(opts, build.WithCreationTime(*creationTime))
	}
	return opts, nil
}

func resolveFilesToWriter(fo *FilenameOptions, no *NameOptions, lo *LocalOptions, out io.Writer) {
	fs, err := enumerateFiles(fo)
	if err != nil {
		log.Fatalf("error enumerating files: %v", err)
	}

	opt, err := gobuildOptions()
	if err != nil {
		log.Fatalf("error setting up builder options: %v", err)
	}
	var sm sync.Map
	wg := sync.WaitGroup{}
	for _, f := range fs {
		wg.Add(1)
		go func(f string) {
			defer wg.Done()

			b, err := resolveFile(f, no, lo, opt...)
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

func resolveFile(f string, no *NameOptions, lo *LocalOptions, opt ...build.Option) ([]byte, error) {
	var pub publish.Interface
	repoName := os.Getenv("KO_DOCKER_REPO")
	if lo.Local || repoName == publish.LocalDomain {
		pub = publish.NewDaemon(daemon.WriteOptions{})
	} else {
		_, err := name.NewRepository(repoName, name.WeakValidation)
		if err != nil {
			return nil, fmt.Errorf("the environment variable KO_DOCKER_REPO must be set to a valid docker repository, got %v", err)
		}

		opts := []publish.Option{publish.WithAuthFromKeychain(authn.DefaultKeychain)}
		if no.PreserveImportPaths {
			opts = append(opts, publish.WithNamer(preserveImportPath))
		} else {
			opts = append(opts, publish.WithNamer(packageWithMD5))
		}

		pub, err = publish.NewDefault(repoName, opts...)
		if err != nil {
			return nil, err
		}
	}

	b, err := ioutil.ReadFile(f)
	if err != nil {
		return nil, err
	}

	builder, err := build.NewGo(opt...)
	if err != nil {
		return nil, err
	}

	return resolve.ImageReferences(b, builder, pub)
}
