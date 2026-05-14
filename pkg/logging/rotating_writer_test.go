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
	"os"
	"sync"
	"testing"
)

func TestRotatingWriter(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := "test.log"

	writer, err := newRotatingWriter(tmpDir, logFile, 10, 24, 7, false)
	if err != nil {
		t.Fatalf("newRotatingWriter failed: %v", err)
	}
	defer writer.Close()

	// Test Write
	data := []byte("test log entry\n")
	n, err := writer.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(data) {
		t.Fatalf("Write returned wrong length: got %d, want %d", n, len(data))
	}

	// Test Sync
	err = writer.Sync()
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
}

func TestRotatingWriterSync(t *testing.T) {
	tmpDir := t.TempDir()

	writer, err := newRotatingWriter(tmpDir, "sync_test.log", 10, 24, 7, false)
	if err != nil {
		t.Fatalf("newRotatingWriter failed: %v", err)
	}
	defer writer.Close()

	var wg sync.WaitGroup
	wg.Add(10)

	// Concurrent writes
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			writer.Write([]byte("test\n"))
		}()
	}

	wg.Wait()

	err = writer.Sync()
	if err != nil {
		t.Fatalf("Sync failed after concurrent writes: %v", err)
	}
}

func TestSetupRotatingFileWriter(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &RotationConfig{
		LogDir:         tmpDir,
		LogFileName:    "app.log",
		LogMaxSizeMB:   10,
		LogMaxAgeHours: 24,
		LogMaxBackups:  7,
		LogCompress:    false,
	}

	writer, err := SetupRotatingFileWriter(cfg)
	if err != nil {
		t.Fatalf("SetupRotatingFileWriter failed: %v", err)
	}

	if writer == nil {
		t.Fatal("SetupRotatingFileWriter returned nil writer")
	}

	_, ok := writer.(*rotatingWriter)
	if !ok {
		t.Fatal("Writer is not a rotatingWriter")
	}
}

func TestSetupRotatingFileWriterFallback(t *testing.T) {
	// Use unwritable directory to trigger fallback
	cfg := &RotationConfig{
		LogDir:         "/proc/invalid_dir_12345",
		LogFileName:    "fallback_test.log",
		LogMaxSizeMB:   10,
		LogMaxAgeHours: 24,
		LogMaxBackups:  7,
		LogCompress:    false,
	}

	writer, err := SetupRotatingFileWriter(cfg)
	if err != nil {
		t.Fatalf("SetupRotatingFileWriter fallback should not fail: %v", err)
	}

	if writer == nil {
		t.Fatal("Fallback writer should not be nil")
	}
}

func TestGetRotationConfig(t *testing.T) {
	// Set environment variables
	os.Setenv("LOG_DIR", "/custom/logs")
	os.Setenv("LOG_FILE_NAME", "custom.log")
	os.Setenv("LOG_MAX_SIZE_MB", "50")
	os.Setenv("LOG_MAX_AGE_HOURS", "48")
	os.Setenv("LOG_MAX_BACKUPS", "100")
	os.Setenv("LOG_COMPRESS", "0")
	os.Setenv("LOG_TIMEZONE", "UTC")
	os.Setenv("LOG_ENABLE_FILE", "false")
	defer func() {
		os.Unsetenv("LOG_DIR")
		os.Unsetenv("LOG_FILE_NAME")
		os.Unsetenv("LOG_MAX_SIZE_MB")
		os.Unsetenv("LOG_MAX_AGE_HOURS")
		os.Unsetenv("LOG_MAX_BACKUPS")
		os.Unsetenv("LOG_COMPRESS")
		os.Unsetenv("LOG_TIMEZONE")
		os.Unsetenv("LOG_ENABLE_FILE")
	}()

	cfg := GetRotationConfig("test-component")

	if cfg.LogDir != "/custom/logs" {
		t.Errorf("LogDir = %s, want /custom/logs", cfg.LogDir)
	}
	if cfg.LogFileName != "custom.log" {
		t.Errorf("LogFileName = %s, want custom.log", cfg.LogFileName)
	}
	if cfg.LogMaxSizeMB != 50 {
		t.Errorf("LogMaxSizeMB = %d, want 50", cfg.LogMaxSizeMB)
	}
	if cfg.LogMaxAgeHours != 48 {
		t.Errorf("LogMaxAgeHours = %d, want 48", cfg.LogMaxAgeHours)
	}
	if cfg.LogMaxBackups != 100 {
		t.Errorf("LogMaxBackups = %d, want 100", cfg.LogMaxBackups)
	}
	if cfg.LogCompress != false {
		t.Errorf("LogCompress = %v, want false", cfg.LogCompress)
	}
	if cfg.LogTimeZone != "UTC" {
		t.Errorf("LogTimeZone = %s, want UTC", cfg.LogTimeZone)
	}
	if cfg.LogEnableFile != false {
		t.Errorf("LogEnableFile = %v, want false", cfg.LogEnableFile)
	}
}

func TestGetRotationConfigDefaults(t *testing.T) {
	// Ensure env vars are not set
	os.Unsetenv("LOG_DIR")
	os.Unsetenv("LOG_FILE_NAME")
	os.Unsetenv("LOG_MAX_SIZE_MB")
	os.Unsetenv("LOG_MAX_AGE_HOURS")
	os.Unsetenv("LOG_MAX_BACKUPS")
	os.Unsetenv("LOG_COMPRESS")
	os.Unsetenv("LOG_TIMEZONE")
	os.Unsetenv("LOG_ENABLE_FILE")

	cfg := GetRotationConfig("mycomponent")

	if cfg.LogDir != "/logs" {
		t.Errorf("LogDir = %s, want /logs", cfg.LogDir)
	}
	if cfg.LogFileName != "mycomponent.log" {
		t.Errorf("LogFileName = %s, want mycomponent.log", cfg.LogFileName)
	}
	if cfg.LogMaxSizeMB != 100 {
		t.Errorf("LogMaxSizeMB = %d, want 100", cfg.LogMaxSizeMB)
	}
	if cfg.LogMaxAgeHours != 24 {
		t.Errorf("LogMaxAgeHours = %d, want 24", cfg.LogMaxAgeHours)
	}
	if cfg.LogMaxBackups != 720 {
		t.Errorf("LogMaxBackups = %d, want 720", cfg.LogMaxBackups)
	}
	if cfg.LogCompress != true {
		t.Errorf("LogCompress = %v, want true", cfg.LogCompress)
	}
	if cfg.LogTimeZone != "Asia/Shanghai" {
		t.Errorf("LogTimeZone = %s, want Asia/Shanghai", cfg.LogTimeZone)
	}
	if cfg.LogEnableFile != true {
		t.Errorf("LogEnableFile = %v, want true (default enabled)", cfg.LogEnableFile)
	}
}

func TestGetEnvIntOrDefault(t *testing.T) {
	os.Setenv("TEST_INT", "42")
	defer os.Unsetenv("TEST_INT")

	result := getEnvIntOrDefault("TEST_INT", 100)
	if result != 42 {
		t.Errorf("getEnvIntOrDefault = %d, want 42", result)
	}

	// Invalid int
	os.Setenv("TEST_INT", "invalid")
	result = getEnvIntOrDefault("TEST_INT", 100)
	if result != 100 {
		t.Errorf("getEnvIntOrDefault with invalid = %d, want 100", result)
	}
}

func TestGetEnvOrDefault(t *testing.T) {
	os.Setenv("TEST_STRING", "custom")
	defer os.Unsetenv("TEST_STRING")

	result := getEnvOrDefault("TEST_STRING", "default")
	if result != "custom" {
		t.Errorf("getEnvOrDefault = %s, want custom", result)
	}

	result = getEnvOrDefault("TEST_STRING_MISSING", "default")
	if result != "default" {
		t.Errorf("getEnvOrDefault missing = %s, want default", result)
	}
}

func TestNewTimeEncoder(t *testing.T) {
	// Test UTC timezone
	enc := NewTimeEncoder("UTC")
	if enc == nil {
		t.Fatal("NewTimeEncoder returned nil")
	}

	// Test invalid timezone falls back to UTC
	enc = NewTimeEncoder("Invalid/Timezone")
	if enc == nil {
		t.Fatal("NewTimeEncoder with invalid timezone returned nil")
	}
}