/*
Copyright 2024 The Knative Authors

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

package logging

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap/zapcore"
)

// RotationConfig holds file rotation settings from environment variables
type RotationConfig struct {
	LogDir         string
	LogFileName    string
	LogMaxSizeMB   int
	LogMaxAgeHours int
	LogMaxBackups  int
	LogCompress    bool
	LogTimeZone    string
	LogEnableFile  bool
}

// GetRotationConfig reads rotation config from environment variables with defaults
func GetRotationConfig(component string) *RotationConfig {
	fileName := getEnvOrDefault("LOG_FILE_NAME", "")
	if fileName == "" {
		// Default: use component name as log file name
		fileName = component + ".log"
	}

	timeZone := getEnvOrDefault("LOG_TIMEZONE", "Asia/Shanghai")

	return &RotationConfig{
		LogDir:         getEnvOrDefault("LOG_DIR", "/logs"),
		LogFileName:    fileName,
		LogMaxSizeMB:   getEnvIntOrDefault("LOG_MAX_SIZE_MB", 100),
		LogMaxAgeHours: getEnvIntOrDefault("LOG_MAX_AGE_HOURS", 24),
		LogMaxBackups:  getEnvIntOrDefault("LOG_MAX_BACKUPS", 720),
		LogCompress:    getEnvOrDefault("LOG_COMPRESS", "1") == "1",
		LogTimeZone:    timeZone,
		LogEnableFile:  getEnvOrDefault("LOG_ENABLE_FILE", "true") == "true", // Default: enabled
	}
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvIntOrDefault(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

// rotatingWriter implements log rotation with gzip compression support
type rotatingWriter struct {
	mu         sync.Mutex
	logDir     string
	baseName   string
	maxSize    int64 // max size in MB
	maxAge     int   // max age in hours
	maxBackups int
	compress   bool
	current    *os.File
	currentSize int64
}

// newRotatingWriter creates a new rotating writer
func newRotatingWriter(logDir, baseName string, maxSizeMB, maxAgeHours, maxBackups int, compress bool) (*rotatingWriter, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	w := &rotatingWriter{
		logDir:     logDir,
		baseName:   baseName,
		maxSize:    int64(maxSizeMB) * 1024 * 1024,
		maxAge:     maxAgeHours,
		maxBackups: maxBackups,
		compress:   compress,
	}

	if err := w.openCurrent(); err != nil {
		return nil, err
	}

	return w, nil
}

func (w *rotatingWriter) openCurrent() error {
	f, err := os.OpenFile(filepath.Join(w.logDir, w.baseName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("failed to stat log file: %w", err)
	}

	w.current = f
	w.currentSize = info.Size()
	return nil
}

// Write implements io.Writer
func (w *rotatingWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.current == nil {
		if err := w.openCurrent(); err != nil {
			return 0, err
		}
	}

	// Check if we need to rotate
	if w.currentSize+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}

	n, err = w.current.Write(p)
	if err != nil {
		return n, err
	}
	w.currentSize += int64(n)

	// Also write to stdout for container log collection
	os.Stdout.Write(p)

	return n, nil
}

// rotate performs log rotation
func (w *rotatingWriter) rotate() error {
	if w.current != nil {
		w.current.Close()
		w.current = nil
	}

	// Generate timestamp for rotated file
	timestamp := time.Now().Format("2006-01-02T15-04-05")
	rotatedName := fmt.Sprintf("%s.%s", w.baseName, timestamp)

	// Rename current to rotated
	oldPath := filepath.Join(w.logDir, w.baseName)
	newPath := filepath.Join(w.logDir, rotatedName)

	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("failed to rename log file: %w", err)
	}

	// Compress if enabled
	if w.compress {
		if err := w.compressFile(newPath); err != nil {
			return fmt.Errorf("failed to compress log file: %w", err)
		}
	}

	// Clean up old files
	if err := w.cleanupOldFiles(); err != nil {
		return fmt.Errorf("failed to cleanup old files: %w", err)
	}

	// Open new current file
	return w.openCurrent()
}

// compressFile compresses a file using gzip
func (w *rotatingWriter) compressFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := os.Create(path + ".gz")
	if err != nil {
		return err
	}
	defer gz.Close()

	gzw := gzip.NewWriter(gz)
	defer gzw.Close()

	_, err = io.Copy(gzw, f)
	return err
}

// cleanupOldFiles removes old log files beyond maxBackups and maxAge
func (w *rotatingWriter) cleanupOldFiles() error {
	pattern := w.baseName + ".*"
	matches, err := filepath.Glob(filepath.Join(w.logDir, pattern))
	if err != nil {
		return err
	}

	// Sort by name (which includes timestamp)
	sort.Strings(matches)

	// Remove files beyond maxBackups
	if len(matches) > w.maxBackups {
		toDelete := len(matches) - w.maxBackups
		for i := 0; i < toDelete; i++ {
			os.Remove(matches[i])
		}
		matches = matches[toDelete:]
	}

	// Remove files older than maxAge
	cutoff := time.Now().Add(-time.Duration(w.maxAge) * time.Hour)
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(match)
		}
	}

	return nil
}

// Sync implements zapcore.WriteSyncer
func (w *rotatingWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.current != nil {
		return w.current.Sync()
	}
	return nil
}

// Close implements io.Closer
func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.current != nil {
		w.current.Close()
		w.current = nil
	}
	return nil
}

var _ zapcore.WriteSyncer = (*rotatingWriter)(nil)

// SetupRotatingFileWriter creates a rotating file writer with fallback to temp dir
func SetupRotatingFileWriter(cfg *RotationConfig) (zapcore.WriteSyncer, error) {
	logDir := cfg.LogDir

	// Directory writability check with fallback to system temp directory
	if err := os.MkdirAll(logDir, 0755); err != nil {
		logDir = filepath.Join(os.TempDir(), "knative-logs")
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create log directory: %w", err)
		}
	}

	return newRotatingWriter(
		logDir,
		cfg.LogFileName,
		cfg.LogMaxSizeMB,
		cfg.LogMaxAgeHours,
		cfg.LogMaxBackups,
		cfg.LogCompress,
	)
}

// NewBeijingTimeEncoder creates a zap time encoder for Beijing timezone
func NewBeijingTimeEncoder() zapcore.TimeEncoder {
	return NewTimeEncoder("Asia/Shanghai")
}

// NewTimeEncoder creates a zap time encoder for specified timezone
func NewTimeEncoder(timeZone string) zapcore.TimeEncoder {
	loc, err := time.LoadLocation(timeZone)
	if err != nil {
		loc = time.UTC
	}
	return func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		localTime := t.In(loc)
		timeStr := localTime.Format("2006-01-02 15:04:05.000")
		enc.AppendString(timeStr)
	}
}