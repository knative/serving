/*
Copyright 2020 The Knative Authors

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

package shell

import (
	"io"
)

// Option overrides configuration options in ExecutorConfig.
type Option func(*ExecutorConfig)

// ProjectLocation represents a project location on a file system.
type ProjectLocation interface {
	RootPath() string
}

// Script represents a script to be executed.
type Script struct {
	Label      string
	ScriptPath string
}

// Function represents a function, whom will be sourced from Script file,
// and executed.
type Function struct {
	Script
	FunctionName string
}

// ExecutorConfig holds executor configuration options.
type ExecutorConfig struct {
	ProjectLocation
	Streams
	Labels
	Environ []string
}

// TestingT is used by testingWriter and allows passing testing.T.
type TestingT interface {
	Logf(format string, args ...any)
}

// testingWriter implements io.Writer and writes to given testing.T log.
type testingWriter struct {
	t TestingT
}

// StreamType represets either output or error stream.
type StreamType int

const (
	// StreamTypeOut represents process output stream.
	StreamTypeOut StreamType = iota
	// StreamTypeErr represents process error stream.
	StreamTypeErr
)

// PrefixFunc is used to build a prefix that will be added to each line of the
// script/function output or error stream.
type PrefixFunc func(st StreamType, label string, config ExecutorConfig) string

// Labels holds a labels to be used to prefix Out and Err streams of executed
// shells scripts/functions.
type Labels struct {
	LabelOut   string
	LabelErr   string
	SkipDate   bool
	DateFormat string
	PrefixFunc
}

// Streams holds a streams of a shell scripts/functions.
type Streams struct {
	Out io.Writer
	Err io.Writer
}

// Executor represents a executor that can execute shell scripts and call
// functions directly.
type Executor interface {
	RunScript(script Script, args ...string) error
	RunFunction(fn Function, args ...string) error
}
