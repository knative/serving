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

package build

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/google/go-containerregistry/v1"
	"github.com/google/go-containerregistry/v1/mutate"
	"github.com/google/go-containerregistry/v1/tarball"
	"github.com/google/go-containerregistry/v1/v1util"
)

var (
	// getwd is a variable for testing.
	getwd = os.Getwd
)

const (
	appPath = "/app"
)

type Options struct {
	// TODO(mattmoor): Architectures?
	GetBase func(string) (v1.Image, error)
}

type gobuild struct {
	importpath string
	opt        Options
	build      func(string) (string, error)
}

func computeImportpath() (string, error) {
	wd, err := getwd()
	if err != nil {
		return "", err
	}
	// Go code lives under $GOPATH/src/...
	src := path.Join(os.Getenv("GOPATH"), "src")
	if !strings.HasPrefix(wd, src) {
		return "", fmt.Errorf("working directory %q must be on GOPATH %q", wd, src)
	}
	return strings.Trim(strings.TrimPrefix(wd, src), "/"), nil
}

// NewGo returns a build.Interface implementation that:
//  1. builds go binaries named by importpath,
//  2. containerizes the binary on a suitable base,
func NewGo(opt Options) (Interface, error) {
	importpath, err := computeImportpath()
	if err != nil {
		return nil, err
	}

	return &gobuild{importpath, opt, build}, nil
}

// IsSupportedReference implements build.Interface
func (gb *gobuild) IsSupportedReference(s string) bool {
	// TODO(mattmoor): Consider supporting vendored things as well.
	return strings.HasPrefix(s, gb.importpath)
}

func build(ip string) (string, error) {
	file, err := ioutil.TempFile(os.TempDir(), "out")
	if err != nil {
		return "", err
	}
	log.Printf("Go building %v", ip)
	cmd := exec.Command("go", "build", "-o", file.Name(), ip)

	// Last one wins
	// TODO(mattmoor): GOARCH=amd64
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux")

	var output bytes.Buffer
	cmd.Stderr = &output
	cmd.Stdout = &output

	if err := cmd.Run(); err != nil {
		os.Remove(file.Name())
		log.Printf("Unexpected error running \"go build\": %v\n%v", err, output.String())
		return "", err
	}
	return file.Name(), nil
}

func tarBinary(binary string) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	tw := tar.NewWriter(buf)
	defer tw.Close()

	file, err := os.Open(binary)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	header := &tar.Header{
		Name: appPath,
		Size: stat.Size(),
		// TODO(mattmoor): Consider a fixed Mode, so that this isn't sensitive
		// to the directory in which it was created.
		Mode: int64(stat.Mode()),
	}
	// write the header to the tarball archive
	if err := tw.WriteHeader(header); err != nil {
		return nil, err
	}
	// copy the file data to the tarball
	if _, err := io.Copy(tw, file); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Build implements build.Interface
func (gb *gobuild) Build(s string) (v1.Image, error) {
	// Do the build into a temporary file.
	file, err := gb.build(s)
	if err != nil {
		return nil, err
	}
	defer os.Remove(file)

	// Construct a tarball with the binary.
	layerBytes, err := tarBinary(file)
	if err != nil {
		return nil, err
	}

	// Create a layer from that tarball.
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return v1util.NopReadCloser(bytes.NewBuffer(layerBytes)), nil
	})
	if err != nil {
		return nil, err
	}

	// Determine the appropriate base image for this import path.
	base, err := gb.opt.GetBase(s)
	if err != nil {
		return nil, err
	}

	// Augment the base image with our application layer.
	withApp, err := mutate.AppendLayers(base, layer)
	if err != nil {
		return nil, err
	}

	// Start from a copy of the base image's config file, and set
	// the entrypoint to our app.
	cfg, err := withApp.ConfigFile()
	if err != nil {
		return nil, err
	}
	cfg = cfg.DeepCopy()
	cfg.Config.Entrypoint = []string{appPath}
	return mutate.Config(withApp, cfg.Config)
}
