package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/transports/alltransports"
)

// Tests the kinds of inputs allowed and expected to the command
func TestDockerRepositoryReferenceParser(t *testing.T) {
	for _, test := range [][]string{
		{"docker://myhost.com:1000/nginx"}, // no tag
		{"docker://myhost.com/nginx"},      // no port or tag
		{"docker://somehost.com"},          // Valid default expansion
		{"docker://nginx"},                 // Valid default expansion
	} {
		ref, err := parseDockerRepositoryReference(test[0])
		require.NoError(t, err)
		expected, err := alltransports.ParseImageName(test[0])
		require.NoError(t, err)
		assert.Equal(t, expected.DockerReference().Name(), ref.DockerReference().Name(), "Mismatched parse result for input %v", test[0])
	}

	for _, test := range [][]string{
		{"oci://somedir"},
		{"dir:/somepath"},
		{"docker-archive:/tmp/dir"},
		{"container-storage:myhost.com/someimage"},
		{"docker-daemon:myhost.com/someimage"},
		{"docker://myhost.com:1000/nginx:foobar:foobar"},           // Invalid repository ref
		{"docker://somehost.com:5000/"},                            // no repo
		{"docker://myhost.com:1000/nginx:latest"},                  // tag not allowed
		{"docker://myhost.com:1000/nginx@sha256:abcdef1234567890"}, // digest not allowed
	} {
		_, err := parseDockerRepositoryReference(test[0])
		assert.Error(t, err, test[0])
	}
}

func TestDockerRepositoryReferenceParserDrift(t *testing.T) {
	for _, test := range [][]string{
		{"docker://myhost.com:1000/nginx", "myhost.com:1000/nginx"}, // no tag
		{"docker://myhost.com/nginx", "myhost.com/nginx"},           // no port or tag
		{"docker://somehost.com", "docker.io/library/somehost.com"}, // Valid default expansion
		{"docker://nginx", "docker.io/library/nginx"},               // Valid default expansion
	} {
		ref, err := parseDockerRepositoryReference(test[0])
		ref2, err2 := alltransports.ParseImageName(test[0])

		if assert.NoError(t, err, "Could not parse, got error on %v", test[0]) && assert.NoError(t, err2, "Could not parse with regular parser, got error on %v", test[0]) {
			assert.Equal(t, ref.DockerReference().String(), ref2.DockerReference().String(), "Different parsing output for input %v. Repo parse = %v, regular parser = %v", test[0], ref, ref2)
		}
	}
}

func TestListTags(t *testing.T) {
	// Invalid command-line arguments
	for _, args := range [][]string{
		{},
		{"a1", "a2"},
	} {
		out, err := runSkopeo(append([]string{"list-tags"}, args...)...)
		assertTestFailed(t, out, err, "exactly one non-option argument expected")
	}

	// FIXME: Much more test coverage
	// Actual feature tests exist in systemtest
}
