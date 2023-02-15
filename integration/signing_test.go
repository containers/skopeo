package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/containers/image/v5/signature"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

const (
	gpgBinary = "gpg"
)

func TestSigning(t *testing.T) {
	suite.Run(t, &signingSuite{})
}

type signingSuite struct {
	suite.Suite
	fingerprint string
}

var _ = suite.SetupAllSuite(&signingSuite{})

func findFingerprint(lineBytes []byte) (string, error) {
	lines := string(lineBytes)
	for _, line := range strings.Split(lines, "\n") {
		fields := strings.Split(line, ":")
		if len(fields) >= 10 && fields[0] == "fpr" {
			return fields[9], nil
		}
	}
	return "", errors.New("No fingerprint found")
}

func (s *signingSuite) SetupSuite() {
	t := s.T()
	_, err := exec.LookPath(skopeoBinary)
	require.NoError(t, err)

	gpgHome := t.TempDir()
	t.Setenv("GNUPGHOME", gpgHome)

	runCommandWithInput(t, "Key-Type: RSA\nName-Real: Testing user\n%no-protection\n%commit\n", gpgBinary, "--homedir", gpgHome, "--batch", "--gen-key")

	lines, err := exec.Command(gpgBinary, "--homedir", gpgHome, "--with-colons", "--no-permission-warning", "--fingerprint").Output()
	require.NoError(t, err)
	s.fingerprint, err = findFingerprint(lines)
	require.NoError(t, err)
}

func (s *signingSuite) TestSignVerifySmoke() {
	t := s.T()
	mech, _, err := signature.NewEphemeralGPGSigningMechanism([]byte{})
	require.NoError(t, err)
	defer mech.Close()
	if err := mech.SupportsSigning(); err != nil { // FIXME? Test that verification and policy enforcement works, using signatures from fixtures
		t.Skipf("Signing not supported: %v", err)
	}

	manifestPath := "fixtures/image.manifest.json"
	dockerReference := "testing/smoketest"

	sigOutput, err := os.CreateTemp("", "sig")
	require.NoError(t, err)
	defer os.Remove(sigOutput.Name())
	assertSkopeoSucceeds(t, "^$", "standalone-sign", "-o", sigOutput.Name(),
		manifestPath, dockerReference, s.fingerprint)

	expected := fmt.Sprintf("^Signature verified, digest %s\n$", TestImageManifestDigest)
	assertSkopeoSucceeds(t, expected, "standalone-verify", manifestPath,
		dockerReference, s.fingerprint, sigOutput.Name())
}
