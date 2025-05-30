package main

import (
	"fmt"
	"os/exec"
	"testing"

	"github.com/containers/skopeo/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

const (
	privateRegistryURL0 = "127.0.0.1:5000"
	privateRegistryURL1 = "127.0.0.1:5001"
)

func TestSkopeo(t *testing.T) {
	suite.Run(t, &skopeoSuite{})
}

type skopeoSuite struct {
	suite.Suite
	regV2         *testRegistryV2
	regV2WithAuth *testRegistryV2
}

var _ = suite.SetupAllSuite(&skopeoSuite{})
var _ = suite.TearDownAllSuite(&skopeoSuite{})

func (s *skopeoSuite) SetupSuite() {
	t := s.T()
	_, err := exec.LookPath(skopeoBinary)
	require.NoError(t, err)
	s.regV2 = setupRegistryV2At(t, privateRegistryURL0, false, false)
	s.regV2WithAuth = setupRegistryV2At(t, privateRegistryURL1, true, false)
}

func (s *skopeoSuite) TearDownSuite() {
	if s.regV2 != nil {
		s.regV2.tearDown()
	}
	if s.regV2WithAuth != nil {
		// cmd := exec.Command("docker", "logout", s.regV2WithAuth)
		// require.Noerror(t, cmd.Run())
		s.regV2WithAuth.tearDown()
	}
}

func (s *skopeoSuite) TestVersion() {
	t := s.T()
	assertSkopeoSucceeds(t, fmt.Sprintf(".*%s version %s.*", skopeoBinary, version.Version),
		"--version")
}

func (s *skopeoSuite) TestCanAuthToPrivateRegistryV2WithoutDockerCfg() {
	t := s.T()
	assertSkopeoFails(t, ".*manifest unknown.*",
		"--tls-verify=false", "inspect", "--creds="+s.regV2WithAuth.username+":"+s.regV2WithAuth.password, fmt.Sprintf("docker://%s/busybox:latest", s.regV2WithAuth.url))
}

func (s *skopeoSuite) TestNeedAuthToPrivateRegistryV2WithoutDockerCfg() {
	t := s.T()
	assertSkopeoFails(t, ".*authentication required.*",
		"--tls-verify=false", "inspect", fmt.Sprintf("docker://%s/busybox:latest", s.regV2WithAuth.url))
}

func (s *skopeoSuite) TestCertDirInsteadOfCertPath() {
	t := s.T()
	assertSkopeoFails(t, ".*unknown flag: --cert-path.*",
		"--tls-verify=false", "inspect", fmt.Sprintf("docker://%s/busybox:latest", s.regV2WithAuth.url), "--cert-path=/")
	assertSkopeoFails(t, ".*authentication required.*",
		"--tls-verify=false", "inspect", fmt.Sprintf("docker://%s/busybox:latest", s.regV2WithAuth.url), "--cert-dir=/etc/docker/certs.d/")
}

// TODO(runcom): as soon as we can push to registries ensure you can inspect here
// not just get image not found :)
func (s *skopeoSuite) TestNoNeedAuthToPrivateRegistryV2ImageNotFound() {
	t := s.T()
	out, err := exec.Command(skopeoBinary, "--tls-verify=false", "inspect", fmt.Sprintf("docker://%s/busybox:latest", s.regV2.url)).CombinedOutput()
	assert.Error(t, err, "%s", string(out))
	assert.Regexp(t, "(?s).*manifest unknown.*", string(out))                         // (?s) : '.' will also match newlines
	assert.NotRegexp(t, "(?s).*unauthorized: authentication required.*", string(out)) // (?s) : '.' will also match newlines
}

func (s *skopeoSuite) TestInspectFailsWhenReferenceIsInvalid() {
	t := s.T()
	assertSkopeoFails(t, `.*Invalid image name.*`, "inspect", "unknown")
}

func (s *skopeoSuite) TestLoginLogout() {
	t := s.T()
	assertSkopeoSucceeds(t, "^Login Succeeded!\n$",
		"login", "--tls-verify=false", "--username="+s.regV2WithAuth.username, "--password="+s.regV2WithAuth.password, s.regV2WithAuth.url)
	// test --get-login returns username
	assertSkopeoSucceeds(t, fmt.Sprintf("^%s\n$", s.regV2WithAuth.username),
		"login", "--tls-verify=false", "--get-login", s.regV2WithAuth.url)
	// test logout
	assertSkopeoSucceeds(t, fmt.Sprintf("^Removed login credentials for %s\n$", s.regV2WithAuth.url),
		"logout", s.regV2WithAuth.url)
}

func (s *skopeoSuite) TestCopyWithLocalAuth() {
	t := s.T()
	assertSkopeoSucceeds(t, "^Login Succeeded!\n$",
		"login", "--tls-verify=false", "--username="+s.regV2WithAuth.username, "--password="+s.regV2WithAuth.password, s.regV2WithAuth.url)
	// copy to private registry using local authentication
	imageName := fmt.Sprintf("docker://%s/busybox:mine", s.regV2WithAuth.url)
	assertSkopeoSucceeds(t, "", "copy", "--dest-tls-verify=false", "--retry-times", "3",
		testFQIN+":latest", imageName)
	// inspect from private registry
	assertSkopeoSucceeds(t, "", "inspect", "--tls-verify=false", imageName)
	// logout from the registry
	assertSkopeoSucceeds(t, fmt.Sprintf("^Removed login credentials for %s\n$", s.regV2WithAuth.url),
		"logout", s.regV2WithAuth.url)
	// inspect from private registry should fail after logout
	assertSkopeoFails(t, ".*authentication required.*",
		"inspect", "--tls-verify=false", imageName)
}
