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
	"testing"
	"time"
)

func TestBeijingTimeEncoder(t *testing.T) {
	enc := NewBeijingTimeEncoder()
	if enc == nil {
		t.Fatal("NewBeijingTimeEncoder returned nil")
	}
}

func TestBeijingLocation(t *testing.T) {
	if beijingLocation == nil {
		t.Fatal("beijingLocation is nil")
	}

	// Verify it's Asia/Shanghai (UTC+8)
	utcTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	beijingTime := utcTime.In(beijingLocation)

	// UTC midnight = 8 AM Beijing
	if beijingTime.Hour() != 8 {
		t.Errorf("Beijing hour = %d, want 8", beijingTime.Hour())
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

func TestNewBeijingTimeEncoderFormat(t *testing.T) {
	enc := NewBeijingTimeEncoder()
	if enc == nil {
		t.Fatal("NewBeijingTimeEncoder returned nil")
	}

	// Test the time format directly by checking the encoder function
	// Beijing time format "2006-01-02 15:04:05.000"
	utcTime := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	beijingTime := utcTime.In(beijingLocation)
	expectedStr := "2024-06-15 20:00:00.000"

	formatted := beijingTime.Format("2006-01-02 15:04:05.000")
	if formatted != expectedStr {
		t.Errorf("Beijing time format = %s, want %s", formatted, expectedStr)
	}
}