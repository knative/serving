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

package types

import (
	"net/http"
	"time"

	"github.com/knative/pkg/ptr"
)

// MustEnvVars defines environment variables that "MUST" be set.
// The value provided is an example value.
var MustEnvVars = map[string]string{
	"PORT": "8801",
}

// ShouldEnvVars defines environment variables that "SHOULD" be set.
// To match these values with test service parameters,
// map values must represent corresponding test.ResourceNames fields
var ShouldEnvVars = map[string]string{
	"K_SERVICE":       "Service",
	"K_CONFIGURATION": "Config",
	"K_REVISION":      "Revision",
}

// MustFiles specifies the file paths and expected permissions that MUST be set as specified in the runtime contract.
// See https://golang.org/pkg/os/#FileMode for "Mode" string meaning. '*' indicates no specification.
var MustFiles = map[string]FileInfo{
	"/dev/fd": {
		IsDir:      ptr.Bool(true),
		SourceFile: "/proc/self/fd",
	},
	"/dev/full": {
		IsDir: ptr.Bool(false),
	},
	// TODO(#822): Add conformance tests for "/dev/log" once implemented
	//"/dev/log": {
	//},
	"/dev/null": {
		IsDir: ptr.Bool(false),
	},
	"/dev/ptmx": {
		IsDir: ptr.Bool(false),
	},
	"/dev/random": {
		IsDir: ptr.Bool(false),
	},
	"/dev/stdin": {
		IsDir:      ptr.Bool(false),
		SourceFile: "/proc/self/fd/0",
	},
	"/dev/stdout": {
		IsDir:      ptr.Bool(false),
		SourceFile: "/proc/self/fd/1",
	},
	"/dev/stderr": {
		IsDir:      ptr.Bool(false),
		SourceFile: "/proc/self/fd/2",
	},
	"/dev/tty": {
		IsDir: ptr.Bool(false),
	},
	"/dev/urandom": {
		IsDir: ptr.Bool(false),
	},
	"/dev/zero": {
		IsDir: ptr.Bool(false),
	},
	"/proc/self/fd": {
		IsDir: ptr.Bool(true),
	},
	"/proc/self/fd/0": {
		IsDir: ptr.Bool(false),
	},
	"/proc/self/fd/1": {
		IsDir: ptr.Bool(false),
	},
	"/proc/self/fd/2": {
		IsDir: ptr.Bool(false),
	},
	"/tmp": {
		IsDir: ptr.Bool(true),
		Perm:  "rwxrwxrwx",
	},
	"/var/log": {
		IsDir: ptr.Bool(true),
		Perm:  "rwxrwxrwx",
	},
}

// ShouldFiles specifies the file paths and expected permissions that SHOULD be set as specified in the runtime contract.
// See https://golang.org/pkg/os/#FileMode for "Mode" string meaning. '*' indicates no specification.
var ShouldFiles = map[string]FileInfo{
	"/etc/resolv.conf": {
		IsDir: ptr.Bool(false),
		Perm:  "rw*r**r**",
	},
	"/dev/console": { // This file SHOULD NOT exist.
		Error: "stat /dev/console: no such file or directory",
	},
}

// RuntimeInfo encapsulates both the host and request information.
type RuntimeInfo struct {
	// Request is information about the request.
	Request *RequestInfo `json:"request"`
	// Host is a set of host information.
	Host *HostInfo `json:"host"`
}

// RequestInfo encapsulates information about the request.
type RequestInfo struct {
	// Ts is the timestamp of when the request came in from the system time.
	Ts time.Time `json:"ts"`
	// URI is the request-target of the Request-Line.
	URI string `json:"uri"`
	// Host is the hostname on which the URL is sought.
	Host string `json:"host"`
	// Method is the method used for the request.
	Method string `json:"method"`
	// Headers is a Map of all headers set.
	Headers http.Header `json:"headers"`
}

// HostInfo contains information about the host environment.
type HostInfo struct {
	// Files is a map of file metadata.
	Files map[string]FileInfo `json:"files"`
	// EnvVars is a map of all environment variables set.
	EnvVars map[string]string `json:"envs"`
	// Cgroups is a list of cgroup information.
	Cgroups []*Cgroup `json:"cgroups"`
	// Mounts is a list of mounted volume information, or error.
	Mounts []*Mount  `json:"mounts"`
	Stdin  *Stdin    `json:"stdin"`
	User   *UserInfo `json:"user"`
}

// Stdin contains information about the Stdin file descriptor for the container.
type Stdin struct {
	// EOF is true if the first byte read from stdin results in EOF.
	EOF *bool `json:"eof,omitempty"`
	// Error is the String representation of an error probing sdtin.
	Error string `json:"error,omitempty"`
}

// UserInfo container information about the current user and group for the running process.
type UserInfo struct {
	UID  int `json:"uid"`
	EUID int `json:"euid"`
	GID  int `json:"gid"`
	EGID int `json:"egid"`
}

// FileInfo contains the metadata for a given file.
type FileInfo struct {
	// Size is the length in bytes for regular files; system-dependent for others.
	Size *int64 `json:"size,omitempty"`
	// Perm are the unix permission bits.
	Perm string `json:"mode,omitempty"`
	// ModTime is the file last modified time.
	ModTime time.Time `json:"modTime,omitempty"`
	// SourceFile is populated if this file is a symlink. The SourceFile is the file where
	// the symlink resolves.
	SourceFile string `json:"sourceFile,omitempty"`
	// IsDir is true if the file is a directory.
	IsDir *bool `json:"isDir,omitempty"`
	// Error is the String representation of the error returned obtaining the information.
	Error string `json:"error,omitempty"`
}

// Cgroup contains the Cgroup value for a given setting.
type Cgroup struct {
	// Name is the full path name of the cgroup.
	Name string `json:"name"`
	// Value is the integer files in the cgroup file.
	Value *int `json:"value,omitempty"`
	// ReadOnly is true if the cgroup was not writable.
	ReadOnly *bool `json:"readOnly,omitempty"`
	// Error is the String representation of the error returned obtaining the information.
	Error string `json:"error,omitempty"`
}

// Mount contains information about a given mount.
type Mount struct {
	// Device is the device that is mounted
	Device string `json:"device,omitempty"`
	// Path is the location where the volume is mounted
	Path string `json:"path,omitempty"`
	// Type is the filesystem type (i.e. sysfs, proc, tmpfs, ext4, overlay, etc.)
	Type string `json:"type,omitempty"`
	// Options is the mount options set (i.e. rw, nosuid, relatime, etc.)
	Options []string `json:"options,omitempty"`
	// Error is the String representation of the error returned obtaining the information.
	Error string `json:"error,omitempty"`
}
