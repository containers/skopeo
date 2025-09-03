package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/signature"
)

const (
	// fixturesTestImageManifestDigest is the Docker manifest digest of "image.manifest.json"
	fixturesTestImageManifestDigest = digest.Digest("sha256:20bf21ed457b390829cdbeec8795a7bea1626991fda603e0d01b4e7f60427e55")
	// fixturesTestKeyFingerprint is the fingerprint of the private key.
	fixturesTestKeyFingerprint = "08CD26E446E2E95249B7A405E932F44B23E8DD43"
	// fixturesTestKeyFingerprint is the key ID of the private key.
	fixturesTestKeyShortID = "E932F44B23E8DD43"
)

// Test that results of runSkopeo failed with nothing on stdout, and substring
// within the error message.
func assertTestFailed(t *testing.T, stdout string, err error, substring string) {
	assert.ErrorContains(t, err, substring)
	assert.Empty(t, stdout)
}

func TestStandaloneSign(t *testing.T) {
	t.Setenv("GNUPGHOME", "fixtures")
	mech, err := signature.NewGPGSigningMechanism()
	require.NoError(t, err)
	defer mech.Close()
	if err := mech.SupportsSigning(); err != nil {
		t.Skipf("Signing not supported: %v", err)
	}

	manifestPath := "fixtures/image.manifest.json"
	dockerReference := "testing/manifest"

	// Invalid command-line arguments
	for _, args := range [][]string{
		{},
		{"a1", "a2"},
		{"a1", "a2", "a3"},
		{"a1", "a2", "a3", "a4"},
		{"-o", "o", "a1", "a2"},
		{"-o", "o", "a1", "a2", "a3", "a4"},
	} {
		out, err := runSkopeo(append([]string{"standalone-sign"}, args...)...)
		assertTestFailed(t, out, err, "Usage")
	}

	// Error reading manifest
	out, err := runSkopeo("standalone-sign", "-o", "/dev/null",
		"/this/does/not/exist", dockerReference, fixturesTestKeyFingerprint)
	assertTestFailed(t, out, err, "/this/does/not/exist")

	// Invalid Docker reference
	out, err = runSkopeo("standalone-sign", "-o", "/dev/null",
		manifestPath, "" /* empty reference */, fixturesTestKeyFingerprint)
	assertTestFailed(t, out, err, "empty signature content")

	// Unknown key.
	out, err = runSkopeo("standalone-sign", "-o", "/dev/null",
		manifestPath, dockerReference, "UNKNOWN GPG FINGERPRINT")
	assert.Error(t, err)
	assert.Empty(t, out)

	// Error writing output
	out, err = runSkopeo("standalone-sign", "-o", "/dev/full",
		manifestPath, dockerReference, fixturesTestKeyFingerprint)
	assertTestFailed(t, out, err, "/dev/full")

	// Success
	sigOutput, err := os.CreateTemp("", "sig")
	require.NoError(t, err)
	defer os.Remove(sigOutput.Name())
	out, err = runSkopeo("standalone-sign", "-o", sigOutput.Name(),
		manifestPath, dockerReference, fixturesTestKeyFingerprint)
	require.NoError(t, err)
	assert.Empty(t, out)

	sig, err := os.ReadFile(sigOutput.Name())
	require.NoError(t, err)
	manifest, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	verified, err := signature.VerifyDockerManifestSignature(sig, manifest, dockerReference, mech, fixturesTestKeyFingerprint)
	require.NoError(t, err)
	assert.Equal(t, dockerReference, verified.DockerReference)
	assert.Equal(t, fixturesTestImageManifestDigest, verified.DockerManifestDigest)
}

func TestStandaloneVerify(t *testing.T) {
	manifestPath := "fixtures/image.manifest.json"
	signaturePath := "fixtures/image.signature"
	dockerReference := "testing/manifest"
	t.Setenv("GNUPGHOME", "fixtures")

	// Invalid command-line arguments
	for _, args := range [][]string{
		{},
		{"a1", "a2", "a3"},
		{"a1", "a2", "a3", "a4", "a5"},
	} {
		out, err := runSkopeo(append([]string{"standalone-verify"}, args...)...)
		assertTestFailed(t, out, err, "Usage")
	}

	// Error reading manifest
	out, err := runSkopeo("standalone-verify", "/this/does/not/exist",
		dockerReference, fixturesTestKeyFingerprint, signaturePath)
	assertTestFailed(t, out, err, "/this/does/not/exist")

	// Error reading signature
	out, err = runSkopeo("standalone-verify", manifestPath,
		dockerReference, fixturesTestKeyFingerprint, "/this/does/not/exist")
	assertTestFailed(t, out, err, "/this/does/not/exist")

	// Error verifying signature
	out, err = runSkopeo("standalone-verify", manifestPath,
		dockerReference, fixturesTestKeyFingerprint, "fixtures/corrupt.signature")
	assertTestFailed(t, out, err, "Error verifying signature")

	// Error using any without a public key file
	out, err = runSkopeo("standalone-verify", manifestPath,
		dockerReference, "any", signaturePath)
	assertTestFailed(t, out, err, "Cannot use any fingerprint without a public key file")

	// Success
	out, err = runSkopeo("standalone-verify", manifestPath,
		dockerReference, fixturesTestKeyFingerprint, signaturePath)
	assert.NoError(t, err)
	assert.Equal(t, "Signature verified using fingerprint "+fixturesTestKeyFingerprint+", digest "+fixturesTestImageManifestDigest.String()+"\n", out)

	// Using multiple fingerprints
	out, err = runSkopeo("standalone-verify", manifestPath,
		dockerReference, "0123456789ABCDEF0123456789ABCDEF01234567,"+fixturesTestKeyFingerprint+",DEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEF", signaturePath)
	assert.NoError(t, err)
	assert.Equal(t, "Signature verified using fingerprint "+fixturesTestKeyFingerprint+", digest "+fixturesTestImageManifestDigest.String()+"\n", out)

	// Using a public key file
	t.Setenv("GNUPGHOME", "")
	out, err = runSkopeo("standalone-verify", "--public-key-file", "fixtures/pubring.gpg", manifestPath,
		dockerReference, fixturesTestKeyFingerprint, signaturePath)
	assert.NoError(t, err)
	assert.Equal(t, "Signature verified using fingerprint "+fixturesTestKeyFingerprint+", digest "+fixturesTestImageManifestDigest.String()+"\n", out)

	// Using a public key file matching any public key
	t.Setenv("GNUPGHOME", "")
	out, err = runSkopeo("standalone-verify", "--public-key-file", "fixtures/pubring.gpg", manifestPath,
		dockerReference, "any", signaturePath)
	assert.NoError(t, err)
	assert.Equal(t, "Signature verified using fingerprint "+fixturesTestKeyFingerprint+", digest "+fixturesTestImageManifestDigest.String()+"\n", out)
}

func TestUntrustedSignatureDump(t *testing.T) {
	// Invalid command-line arguments
	for _, args := range [][]string{
		{},
		{"a1", "a2"},
		{"a1", "a2", "a3", "a4"},
	} {
		out, err := runSkopeo(append([]string{"untrusted-signature-dump-without-verification"}, args...)...)
		assertTestFailed(t, out, err, "Usage")
	}

	// Error reading manifest
	out, err := runSkopeo("untrusted-signature-dump-without-verification",
		"/this/does/not/exist")
	assertTestFailed(t, out, err, "/this/does/not/exist")

	// Error reading signature (input is not a signature)
	out, err = runSkopeo("untrusted-signature-dump-without-verification", "fixtures/image.manifest.json")
	assertTestFailed(t, out, err, "Error decoding untrusted signature")

	// Success
	for _, path := range []string{"fixtures/image.signature", "fixtures/corrupt.signature"} {
		// Success
		out, err = runSkopeo("untrusted-signature-dump-without-verification", path)
		require.NoError(t, err)

		var info signature.UntrustedSignatureInformation
		err := json.Unmarshal([]byte(out), &info)
		require.NoError(t, err)
		assert.Equal(t, fixturesTestImageManifestDigest, info.UntrustedDockerManifestDigest)
		assert.Equal(t, "testing/manifest", info.UntrustedDockerReference)
		assert.NotNil(t, info.UntrustedCreatorID)
		assert.Equal(t, "atomic ", *info.UntrustedCreatorID)
		assert.NotNil(t, info.UntrustedTimestamp)
		assert.True(t, time.Unix(1458239713, 0).Equal(*info.UntrustedTimestamp))
		assert.Equal(t, fixturesTestKeyShortID, info.UntrustedShortKeyIdentifier)
	}
}
