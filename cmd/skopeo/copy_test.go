package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/copy"
)

func TestCopy(t *testing.T) {
	// Invalid command-line arguments
	for _, args := range [][]string{
		{},
		{"a1"},
		{"a1", "a2", "a3"},
	} {
		out, err := runSkopeo(append([]string{"--insecure-policy", "copy"}, args...)...)
		assertTestFailed(t, out, err, "exactly two arguments expected")
	}

	// FIXME: Much more test coverage
	// Actual feature tests exist in integration and systemtest
}

func TestParseMultiArch(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		expectedSelection copy.ImageListSelection
		expectedPlatforms []copy.InstancePlatformFilter
		expectError       bool
	}{
		{
			name:              "system option",
			input:             "system",
			expectedSelection: copy.CopySystemImage,
			expectedPlatforms: nil,
			expectError:       false,
		},
		{
			name:              "all option",
			input:             "all",
			expectedSelection: copy.CopyAllImages,
			expectedPlatforms: nil,
			expectError:       false,
		},
		{
			name:              "index-only option",
			input:             "index-only",
			expectedSelection: copy.CopySpecificImages,
			expectedPlatforms: nil,
			expectError:       false,
		},
		{
			name:              "single platform",
			input:             "linux/amd64",
			expectedSelection: copy.CopySpecificImages,
			expectedPlatforms: []copy.InstancePlatformFilter{
				{OS: "linux", Architecture: "amd64"},
			},
			expectError: false,
		},
		{
			name:              "multiple platforms",
			input:             "linux/amd64,linux/arm64",
			expectedSelection: copy.CopySpecificImages,
			expectedPlatforms: []copy.InstancePlatformFilter{
				{OS: "linux", Architecture: "amd64"},
				{OS: "linux", Architecture: "arm64"},
			},
			expectError: false,
		},
		{
			name:              "platforms with whitespace",
			input:             "linux/amd64, linux/arm64 , windows/amd64",
			expectedSelection: copy.CopySpecificImages,
			expectedPlatforms: []copy.InstancePlatformFilter{
				{OS: "linux", Architecture: "amd64"},
				{OS: "linux", Architecture: "arm64"},
				{OS: "windows", Architecture: "amd64"},
			},
			expectError: false,
		},
		{
			name:        "invalid option",
			input:       "invalid",
			expectError: true,
		},
		{
			name:        "invalid platform format - no slash",
			input:       "linux-amd64",
			expectError: true,
		},
		{
			name:        "invalid platform format - too many parts",
			input:       "linux/amd64/extra",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selection, platforms, err := parseMultiArch(tt.input)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedSelection, selection)
			assert.Equal(t, tt.expectedPlatforms, platforms)
		})
	}
}
