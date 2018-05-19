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
	"log"
	"net/http"

	"github.com/spf13/viper"

	"github.com/google/go-containerregistry/authn"
	"github.com/google/go-containerregistry/name"
	"github.com/google/go-containerregistry/v1"
	"github.com/google/go-containerregistry/v1/remote"
)

var (
	defaultBaseImage   name.Reference
	baseImageOverrides map[string]name.Reference
)

func GetBaseImage(s string) (v1.Image, error) {
	ref, ok := baseImageOverrides[s]
	if !ok {
		ref = defaultBaseImage
	}
	log.Printf("Using base %s for %s", ref, s)
	return remote.Image(ref, authn.Anonymous, http.DefaultTransport)
}

func GetMountPaths() []name.Repository {
	repos := make([]name.Repository, 0, len(baseImageOverrides)+1)
	repos = append(repos, defaultBaseImage.Context())
	for _, v := range baseImageOverrides {
		repos = append(repos, v.Context())
	}
	return repos
}

func init() {
	// If omitted, use this base image.
	viper.SetDefault("defaultBaseImage", "gcr.io/distroless/base:latest")

	viper.SetConfigName(".ko") // .yaml is implicit
	viper.AddConfigPath("./")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			log.Fatalf("error reading config file: %v", err)
		}
	}

	ref := viper.GetString("defaultBaseImage")
	dbi, err := name.ParseReference(ref, name.WeakValidation)
	if err != nil {
		log.Fatalf("'defaultBaseImage': error parsing %q as image reference: %v", ref, err)
	}
	defaultBaseImage = dbi

	baseImageOverrides = make(map[string]name.Reference)
	overrides := viper.GetStringMapString("baseImageOverrides")
	for k, v := range overrides {
		bi, err := name.ParseReference(v, name.WeakValidation)
		if err != nil {
			log.Fatalf("'baseImageOverrides': error parsing %q as image reference: %v", v, err)
		}
		baseImageOverrides[k] = bi
	}
}
