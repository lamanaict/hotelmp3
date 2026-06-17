package main

import (
	"path/filepath"
	"testing"
)

func TestFindExecutable(t *testing.T) {
	// Test with a known executable
	result := findExecutable("go")
	if result == "" {
		// Not necessarily an error — go may not be on PATH in CI
		t.Skip("go not found on PATH, skipping")
	}
	t.Logf("findExecutable('go') = %s", result)
}

func TestMediaExtensions(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"song.mp3", true},
		{"video.mp4", true},
		{"image.jpg", false},
		{"FILE.MP3", true},
		{"noext", false},
	}
	for _, tt := range tests {
		ext := filepath.Ext(tt.path)
		got := mediaExts[ext]
		if got != tt.want {
			t.Errorf("mediaExts[%q] = %v, want %v", ext, got, tt.want)
		}
	}
}
