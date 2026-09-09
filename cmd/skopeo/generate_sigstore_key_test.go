package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateSigstoreKey(t *testing.T) {
	// Invalid command-line arguments
	out, err := runSkopeo("generate-sigstore-key")
	assertTestFailed(t, out, err, "--output-prefix must be specified")
	out, err = runSkopeo("generate-sigstore-key", "--output-prefix", "foo", "a1")
	assertTestFailed(t, out, err, "no arguments expected")

	// One of the destination files already exists
	outputSuffixes := []string{".pub", ".private"}
	for _, suffix := range outputSuffixes {
		dir := t.TempDir()
		prefix := filepath.Join(dir, "prefix")
		err := os.WriteFile(prefix+suffix, []byte{}, 0o600)
		require.NoError(t, err)
		out, err := runSkopeo("generate-sigstore-key",
			"--output-prefix", prefix, "--passphrase-file", "/dev/null",
		)
		assertTestFailed(t, out, err, "refusing to overwrite")
	}

	// One of the destinations is inaccessible (simulate by a symlink that tries to
	// traverse a non-directory)
	for _, suffix := range outputSuffixes {
		dir := t.TempDir()
		nonDirectory := filepath.Join(dir, "nondirectory")
		err := os.WriteFile(nonDirectory, []byte{}, 0o600)
		require.NoError(t, err)
		prefix := filepath.Join(dir, "prefix")
		err = os.Symlink(filepath.Join(nonDirectory, "unaccessible"), prefix+suffix)
		require.NoError(t, err)
		out, err := runSkopeo("generate-sigstore-key",
			"--output-prefix", prefix, "--passphrase-file", "/dev/null",
		)
		assertTestFailed(t, out, err, prefix+suffix) // + an OS-specific error message
	}
	destDir := t.TempDir()
	// Error reading passphrase
	out, err = runSkopeo("generate-sigstore-key",
		"--output-prefix", filepath.Join(destDir, "prefix"),
		"--passphrase-file", filepath.Join(destDir, "this-does-not-exist"),
	)
	assertTestFailed(t, out, err, "this-does-not-exist")

	// (The interactive passphrase prompting is not yet tested)

	// Error writing outputs is untested: when unit tests run as root, we can’t use permissions on a directory to cause write failures,
	// with the --output-prefix mechanism, and refusing to even start writing to pre-exisiting files, directories are the only mechanism
	// we have to trigger a write failure.

	// Success
	// Just a smoke-test, usability of the keys is tested in the generate implementation.
	dir := t.TempDir()
	prefix := filepath.Join(dir, "prefix")
	passphraseFile := filepath.Join(dir, "passphrase")
	err = os.WriteFile(passphraseFile, []byte("some passphrase"), 0o600)
	require.NoError(t, err)
	out, err = runSkopeo("generate-sigstore-key",
		"--output-prefix", prefix, "--passphrase-file", passphraseFile,
	)
	assert.NoError(t, err)
	for _, suffix := range outputSuffixes {
		assert.Contains(t, out, prefix+suffix)
	}
}
