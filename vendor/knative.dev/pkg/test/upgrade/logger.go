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

package upgrade

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"go.uber.org/zap"
)

const testingTScheme = "testing-t"

var (
	// ErrInvalidTestingOutputMethod when given invalid testing output method.
	ErrInvalidTestingOutputMethod = errors.New("invalid testing output method")
	// ErrNoTestingTRegisteredForTest when no testing.TB registered for test.
	ErrNoTestingTRegisteredForTest = errors.New("no testing.TB registered for test")

	testingTeeBees = make(map[string]testing.TB)
)

func (c Configuration) logger(tb testing.TB) (*zap.SugaredLogger, error) {
	// TODO(mgencur): Remove when dependent repositories use LogConfig instead of Log.
	// This is for backwards compatibility.
	if c.Log != nil {
		return c.Log.Sugar(), nil
	}

	testName := tb.Name()
	testingTeeBees[testName] = tb
	cfg := c.logConfig().Config
	outs := []string{fmt.Sprintf("%s://Log/%s", testingTScheme, testName)}
	if isStandardOutputOnly(cfg.OutputPaths) {
		cfg.OutputPaths = outs
	} else {
		cfg.OutputPaths = append(cfg.OutputPaths, outs...)
	}
	errOuts := []string{fmt.Sprintf("%s://Error/%s", testingTScheme, testName)}
	if isStandardOutputOnly(cfg.ErrorOutputPaths) {
		cfg.ErrorOutputPaths = errOuts
	} else {
		cfg.ErrorOutputPaths = append(cfg.ErrorOutputPaths, errOuts...)
	}
	err := zap.RegisterSink(testingTScheme, func(url *url.URL) (zap.Sink, error) {
		var ok bool
		testName = strings.TrimPrefix(url.Path, "/")
		tb, ok = testingTeeBees[testName]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrNoTestingTRegisteredForTest, testName)
		}
		switch url.Host {
		case "Log":
			return sink{writer: tb.Log}, nil
		case "Error":
			return sink{writer: tb.Error}, nil
		}
		return nil, fmt.Errorf("%w: %s", ErrInvalidTestingOutputMethod, url.Host)
	})
	if err != nil && !strings.HasPrefix(err.Error(), "sink factory already registered") {
		return nil, err
	}
	var logger *zap.Logger
	logger, err = cfg.Build(c.logConfig().Options...)
	if err != nil {
		return nil, err
	}
	return logger.Sugar(), nil
}

type sink struct {
	writer func(args ...interface{})
}

func (t sink) Write(bytes []byte) (n int, err error) {
	msg := string(bytes)
	t.writer(msg)
	return len(bytes), nil
}

func (t sink) Sync() error {
	return nil
}

func (t sink) Close() error {
	return nil
}

// isStandardOutputOnly checks the output paths and returns true, if they
// contain only standard outputs.
func isStandardOutputOnly(paths []string) bool {
	return len(paths) == 1 && (paths[0] == "stderr" || paths[0] == "stdout")
}
