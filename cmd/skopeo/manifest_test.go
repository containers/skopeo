package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestManifestDigest(t *testing.T) {
	// Invalid command-line arguments
	for _, args := range [][]string{
		{},
		{"a1", "a2"},
	} {
		out, err := runSkopeo(append([]string{"manifest-digest"}, args...)...)
		assertTestFailed(t, out, err, "exactly one argument expected")
	}

	// Error reading manifest
	out, err := runSkopeo("manifest-digest", "/this/does/not/exist")
	assertTestFailed(t, out, err, "/this/does/not/exist")

	// Error computing manifest
	out, err = runSkopeo("manifest-digest", "fixtures/v2s1-invalid-signatures.manifest.json")
	assertTestFailed(t, out, err, "computing digest")

	// Success
	out, err = runSkopeo("manifest-digest", "fixtures/image.manifest.json")
	assert.NoError(t, err)
	assert.Equal(t, fixturesTestImageManifestDigest.String()+"\n", out)
}
