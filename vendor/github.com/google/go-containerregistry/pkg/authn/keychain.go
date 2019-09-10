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

package authn

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"

	"github.com/google/go-containerregistry/pkg/logs"
)

// Resource represents a registry or repository that can be authenticated against.
type Resource interface {
	// String returns the full string representation of the target, e.g.
	// gcr.io/my-project or just gcr.io.
	String() string

	// RegistryStr returns just the registry portion of the target, e.g. for
	// gcr.io/my-project, this should just return gcr.io. This is needed to
	// pull out an appropriate hostname.
	RegistryStr() string
}

// Keychain is an interface for resolving an image reference to a credential.
type Keychain interface {
	// Resolve looks up the most appropriate credential for the specified target.
	Resolve(Resource) (Authenticator, error)
}

// defaultKeychain implements Keychain with the semantics of the standard Docker
// credential keychain.
type defaultKeychain struct{}

// configDir returns the directory containing Docker's config.json
func configDir() (string, error) {
	if dc := os.Getenv("DOCKER_CONFIG"); dc != "" {
		return dc, nil
	}
	if h := dockerUserHomeDir(); h != "" {
		return filepath.Join(dockerUserHomeDir(), ".docker"), nil
	}
	return "", errNoHomeDir
}

var errNoHomeDir = errors.New("could not determine home directory")

// dockerUserHomeDir returns the current user's home directory, as interpreted by Docker.
func dockerUserHomeDir() string {
	if runtime.GOOS == "windows" {
		// Docker specifically expands "%USERPROFILE%" on Windows,
		return os.Getenv("USERPROFILE")
	}
	// Docker defaults to "$HOME" Linux and OSX.
	return os.Getenv("HOME")
}

// authEntry is a helper for JSON parsing an "auth" entry of config.json
// This is not meant for direct consumption.
type authEntry struct {
	Auth     string `json:"auth"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// cfg is a helper for JSON parsing Docker's config.json
// This is not meant for direct consumption.
type cfg struct {
	CredHelper map[string]string    `json:"credHelpers,omitempty"`
	CredStore  string               `json:"credsStore,omitempty"`
	Auths      map[string]authEntry `json:"auths,omitempty"`
}

// There are a variety of ways a domain may get qualified within the Docker credential file.
// We enumerate them here as format strings.
var (
	domainForms = []string{
		// Allow naked domains
		"%s",
		// Allow scheme-prefixed.
		"https://%s",
		"http://%s",
		// Allow scheme-prefixes with version in url path.
		"https://%s/v1/",
		"http://%s/v1/",
		"https://%s/v2/",
		"http://%s/v2/",
	}

	// Export an instance of the default keychain.
	DefaultKeychain Keychain = &defaultKeychain{}
)

// Resolve implements Keychain.
func (dk *defaultKeychain) Resolve(target Resource) (Authenticator, error) {
	dir, err := configDir()
	if err != nil {
		logs.Warn.Printf("Unable to determine config dir: %v", err)
		return Anonymous, nil
	}
	file := filepath.Join(dir, "config.json")
	content, err := ioutil.ReadFile(file)
	if err != nil {
		logs.Warn.Printf("Unable to read %q: %v", file, err)
		return Anonymous, nil
	}

	var cf cfg
	if err := json.Unmarshal(content, &cf); err != nil {
		logs.Warn.Printf("Unable to parse %q: %v", file, err)
		return Anonymous, nil
	}

	// Per-registry credential helpers take precedence.
	if cf.CredHelper != nil {
		for _, form := range domainForms {
			if entry, ok := cf.CredHelper[fmt.Sprintf(form, target.RegistryStr())]; ok {
				return &helper{name: entry, domain: target.RegistryStr(), r: &defaultRunner{}}, nil
			}
		}
	}

	// A global credential helper is next in precedence.
	if cf.CredStore != "" {
		return &helper{name: cf.CredStore, domain: target.RegistryStr(), r: &defaultRunner{}}, nil
	}

	// Lastly, the 'auths' section directly contains basic auth entries.
	if cf.Auths != nil {
		for _, form := range domainForms {
			if entry, ok := cf.Auths[fmt.Sprintf(form, target.RegistryStr())]; ok {
				if entry.Auth != "" {
					return &auth{entry.Auth}, nil
				} else if entry.Username != "" {
					return &Basic{Username: entry.Username, Password: entry.Password}, nil
				} else {
					// TODO(mattmoor): Support identitytoken
					// TODO(mattmoor): Support registrytoken
					return nil, fmt.Errorf("Unsupported entry in \"auths\" section of %q", file)
				}
			}
		}
	}

	// Fallback on anonymous.
	return Anonymous, nil
}
