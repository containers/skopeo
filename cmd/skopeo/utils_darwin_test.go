//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetDefaultDockerSocketDarwin(t *testing.T) {
	// Save original HOME
	originalHome := os.Getenv("HOME")
	defer func() {
		os.Setenv("HOME", originalHome)
	}()

	// Test with HOME set
	testHome := "/Users/test"
	os.Setenv("HOME", testHome)
	expected := "unix://" + filepath.Join(testHome, ".docker/run/docker.sock")
	result := getDefaultDockerSocket()
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Test with HOME unset
	os.Unsetenv("HOME")
	result = getDefaultDockerSocket()
	if result != "" {
		t.Errorf("Expected empty string when HOME is unset, got %s", result)
	}
}
