package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/signature"
	"github.com/containers/image/v5/types"
	digest "github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/opencontainers/image-tools/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

const (
	v2DockerRegistryURL   = "localhost:5555" // Update also policy.json
	v2s1DockerRegistryURL = "localhost:5556"
	knownWindowsOnlyImage = "docker://mcr.microsoft.com/windows/nanoserver:1909"
	knownListImageRepo    = "docker://registry.fedoraproject.org/fedora-minimal"
	knownListImage        = knownListImageRepo + ":38"
)

func TestCopy(t *testing.T) {
	suite.Run(t, &copySuite{})
}

type copySuite struct {
	suite.Suite
	cluster    *openshiftCluster
	registry   *testRegistryV2
	s1Registry *testRegistryV2
	gpgHome    string
}

var _ = suite.SetupAllSuite(&copySuite{})
var _ = suite.TearDownAllSuite(&copySuite{})

func (s *copySuite) SetupSuite() {
	t := s.T()
	if os.Getenv("SKOPEO_CONTAINER_TESTS") != "1" {
		t.Skip("Not running in a container, refusing to affect user state")
	}

	s.cluster = startOpenshiftCluster(t) // FIXME: Set up TLS for the docker registry port instead of using "--tls-verify=false" all over the place.

	for _, stream := range []string{"unsigned", "personal", "official", "naming", "cosigned", "compression", "schema1", "schema2"} {
		isJSON := fmt.Sprintf(`{
			"kind": "ImageStream",
			"apiVersion": "v1",
			"metadata": {
			    "name": "%s"
			},
			"spec": {}
		}`, stream)
		runCommandWithInput(t, isJSON, "oc", "create", "-f", "-")
	}

	// FIXME: Set up TLS for the docker registry port instead of using "--tls-verify=false" all over the place.
	s.registry = setupRegistryV2At(t, v2DockerRegistryURL, false, false)
	s.s1Registry = setupRegistryV2At(t, v2s1DockerRegistryURL, false, true)

	s.gpgHome = t.TempDir()
	t.Setenv("GNUPGHOME", s.gpgHome)

	for _, key := range []string{"personal", "official"} {
		batchInput := fmt.Sprintf("Key-Type: RSA\nName-Real: Test key - %s\nName-email: %s@example.com\n%%no-protection\n%%commit\n",
			key, key)
		runCommandWithInput(t, batchInput, gpgBinary, "--batch", "--gen-key")

		out := combinedOutputOfCommand(t, gpgBinary, "--armor", "--export", fmt.Sprintf("%s@example.com", key))
		err := os.WriteFile(filepath.Join(s.gpgHome, fmt.Sprintf("%s-pubkey.gpg", key)),
			[]byte(out), 0600)
		require.NoError(t, err)
	}
}

func (s *copySuite) TearDownSuite() {
	t := s.T()
	if s.registry != nil {
		s.registry.tearDown()
	}
	if s.s1Registry != nil {
		s.s1Registry.tearDown()
	}
	if s.cluster != nil {
		s.cluster.tearDown(t)
	}
}

func (s *copySuite) TestCopyWithManifestList() {
	t := s.T()
	dir := t.TempDir()
	assertSkopeoSucceeds(t, "", "copy", knownListImage, "dir:"+dir)
}

func (s *copySuite) TestCopyAllWithManifestList() {
	t := s.T()
	dir := t.TempDir()
	assertSkopeoSucceeds(t, "", "copy", "--all", knownListImage, "dir:"+dir)
}

func (s *copySuite) TestCopyAllWithManifestListRoundTrip() {
	t := s.T()
	oci1 := t.TempDir()
	oci2 := t.TempDir()
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	assertSkopeoSucceeds(t, "", "copy", "--multi-arch=all", knownListImage, "oci:"+oci1)
	assertSkopeoSucceeds(t, "", "copy", "--multi-arch=all", "oci:"+oci1, "dir:"+dir1)
	assertSkopeoSucceeds(t, "", "copy", "--multi-arch=all", "dir:"+dir1, "oci:"+oci2)
	assertSkopeoSucceeds(t, "", "copy", "--multi-arch=all", "oci:"+oci2, "dir:"+dir2)
	assertDirImagesAreEqual(t, dir1, dir2)
	out := combinedOutputOfCommand(t, "diff", "-urN", oci1, oci2)
	assert.Equal(t, "", out)
}

func (s *copySuite) TestCopyAllWithManifestListConverge() {
	t := s.T()
	oci1 := t.TempDir()
	oci2 := t.TempDir()
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	assertSkopeoSucceeds(t, "", "copy", "--multi-arch=all", knownListImage, "oci:"+oci1)
	assertSkopeoSucceeds(t, "", "copy", "--multi-arch=all", "oci:"+oci1, "dir:"+dir1)
	assertSkopeoSucceeds(t, "", "copy", "--multi-arch=all", "--format", "oci", knownListImage, "dir:"+dir2)
	assertSkopeoSucceeds(t, "", "copy", "--multi-arch=all", "dir:"+dir2, "oci:"+oci2)
	assertDirImagesAreEqual(t, dir1, dir2)
	out := combinedOutputOfCommand(t, "diff", "-urN", oci1, oci2)
	assert.Equal(t, "", out)
}

func (s *copySuite) TestCopyNoneWithManifestList() {
	t := s.T()
	dir1 := t.TempDir()
	assertSkopeoSucceeds(t, "", "copy", "--multi-arch=index-only", knownListImage, "dir:"+dir1)

	manifestPath := filepath.Join(dir1, "manifest.json")
	readManifest, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	mimeType := manifest.GuessMIMEType(readManifest)
	assert.Equal(t, "application/vnd.docker.distribution.manifest.list.v2+json", mimeType)
	out := combinedOutputOfCommand(t, "ls", "-1", dir1)
	assert.Equal(t, "manifest.json\nversion\n", out)
}

func (s *copySuite) TestCopyWithManifestListConverge() {
	t := s.T()
	oci1 := t.TempDir()
	oci2 := t.TempDir()
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	assertSkopeoSucceeds(t, "", "copy", knownListImage, "oci:"+oci1)
	assertSkopeoSucceeds(t, "", "copy", "--multi-arch=all", "oci:"+oci1, "dir:"+dir1)
	assertSkopeoSucceeds(t, "", "copy", "--format", "oci", knownListImage, "dir:"+dir2)
	assertSkopeoSucceeds(t, "", "copy", "--multi-arch=all", "dir:"+dir2, "oci:"+oci2)
	assertDirImagesAreEqual(t, dir1, dir2)
	out := combinedOutputOfCommand(t, "diff", "-urN", oci1, oci2)
	assert.Equal(t, "", out)
}

func (s *copySuite) TestCopyAllWithManifestListStorageFails() {
	t := s.T()
	storage := t.TempDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	assertSkopeoFails(t, `.*destination transport .* does not support copying multiple images as a group.*`, "copy", "--multi-arch=all", knownListImage, "containers-storage:"+storage+"test")
}

func (s *copySuite) TestCopyWithManifestListStorage() {
	t := s.T()
	storage := t.TempDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	assertSkopeoSucceeds(t, "", "copy", knownListImage, "containers-storage:"+storage+"test")
	assertSkopeoSucceeds(t, "", "copy", knownListImage, "dir:"+dir1)
	assertSkopeoSucceeds(t, "", "copy", "containers-storage:"+storage+"test", "dir:"+dir2)
	decompressDirs(t, dir1, dir2)
	assertDirImagesAreEqual(t, dir1, dir2)
}

func (s *copySuite) TestCopyWithManifestListStorageMultiple() {
	t := s.T()
	storage := t.TempDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	assertSkopeoSucceeds(t, "", "--override-arch", "amd64", "copy", knownListImage, "containers-storage:"+storage+"test")
	assertSkopeoSucceeds(t, "", "--override-arch", "arm64", "copy", knownListImage, "containers-storage:"+storage+"test")
	assertSkopeoSucceeds(t, "", "--override-arch", "arm64", "copy", knownListImage, "dir:"+dir1)
	assertSkopeoSucceeds(t, "", "copy", "containers-storage:"+storage+"test", "dir:"+dir2)
	decompressDirs(t, dir1, dir2)
	assertDirImagesAreEqual(t, dir1, dir2)
}

func (s *copySuite) TestCopyWithManifestListDigest() {
	t := s.T()
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	oci1 := t.TempDir()
	oci2 := t.TempDir()
	m := combinedOutputOfCommand(t, skopeoBinary, "inspect", "--raw", knownListImage)
	manifestDigest, err := manifest.Digest([]byte(m))
	require.NoError(t, err)
	digest := manifestDigest.String()
	assertSkopeoSucceeds(t, "", "copy", knownListImageRepo+"@"+digest, "dir:"+dir1)
	assertSkopeoSucceeds(t, "", "copy", "--multi-arch=all", knownListImageRepo+"@"+digest, "dir:"+dir2)
	assertSkopeoSucceeds(t, "", "copy", "dir:"+dir1, "oci:"+oci1)
	assertSkopeoSucceeds(t, "", "copy", "dir:"+dir2, "oci:"+oci2)
	out := combinedOutputOfCommand(t, "diff", "-urN", oci1, oci2)
	assert.Equal(t, "", out)
}

func (s *copySuite) TestCopyWithDigestfileOutput() {
	t := s.T()
	tempdir := t.TempDir()
	dir1 := t.TempDir()
	digestOutPath := filepath.Join(tempdir, "digest.txt")
	assertSkopeoSucceeds(t, "", "copy", "--digestfile="+digestOutPath, knownListImage, "dir:"+dir1)
	readDigest, err := os.ReadFile(digestOutPath)
	require.NoError(t, err)
	_, err = digest.Parse(string(readDigest))
	require.NoError(t, err)
}

func (s *copySuite) TestCopyWithManifestListStorageDigest() {
	t := s.T()
	storage := t.TempDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	m := combinedOutputOfCommand(t, skopeoBinary, "inspect", "--raw", knownListImage)
	manifestDigest, err := manifest.Digest([]byte(m))
	require.NoError(t, err)
	digest := manifestDigest.String()
	assertSkopeoSucceeds(t, "", "copy", knownListImageRepo+"@"+digest, "containers-storage:"+storage+"test@"+digest)
	assertSkopeoSucceeds(t, "", "copy", "containers-storage:"+storage+"test@"+digest, "dir:"+dir1)
	assertSkopeoSucceeds(t, "", "copy", knownListImageRepo+"@"+digest, "dir:"+dir2)
	decompressDirs(t, dir1, dir2)
	assertDirImagesAreEqual(t, dir1, dir2)
}

func (s *copySuite) TestCopyWithManifestListStorageDigestMultipleArches() {
	t := s.T()
	storage := t.TempDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	m := combinedOutputOfCommand(t, skopeoBinary, "inspect", "--raw", knownListImage)
	manifestDigest, err := manifest.Digest([]byte(m))
	require.NoError(t, err)
	digest := manifestDigest.String()
	assertSkopeoSucceeds(t, "", "copy", knownListImageRepo+"@"+digest, "containers-storage:"+storage+"test@"+digest)
	assertSkopeoSucceeds(t, "", "copy", "containers-storage:"+storage+"test@"+digest, "dir:"+dir1)
	assertSkopeoSucceeds(t, "", "copy", knownListImageRepo+"@"+digest, "dir:"+dir2)
	decompressDirs(t, dir1, dir2)
	assertDirImagesAreEqual(t, dir1, dir2)
}

func (s *copySuite) TestCopyWithManifestListStorageDigestMultipleArchesBothUseListDigest() {
	t := s.T()
	storage := t.TempDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	m := combinedOutputOfCommand(t, skopeoBinary, "inspect", "--raw", knownListImage)
	manifestDigest, err := manifest.Digest([]byte(m))
	require.NoError(t, err)
	digest := manifestDigest.String()
	_, err = manifest.ListFromBlob([]byte(m), manifest.GuessMIMEType([]byte(m)))
	require.NoError(t, err)
	assertSkopeoSucceeds(t, "", "--override-arch=amd64", "copy", knownListImageRepo+"@"+digest, "containers-storage:"+storage+"test@"+digest)
	assertSkopeoSucceeds(t, "", "--override-arch=arm64", "copy", knownListImageRepo+"@"+digest, "containers-storage:"+storage+"test@"+digest)
	assertSkopeoFails(t, `.*reading manifest for image instance.*does not exist.*`, "--override-arch=amd64", "inspect", "containers-storage:"+storage+"test@"+digest)
	assertSkopeoFails(t, `.*reading manifest for image instance.*does not exist.*`, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	i2 := combinedOutputOfCommand(t, skopeoBinary, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	var image2 imgspecv1.Image
	err = json.Unmarshal([]byte(i2), &image2)
	require.NoError(t, err)
	assert.Equal(t, "arm64", image2.Architecture)
}

func (s *copySuite) TestCopyWithManifestListStorageDigestMultipleArchesFirstUsesListDigest() {
	t := s.T()
	storage := t.TempDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	m := combinedOutputOfCommand(t, skopeoBinary, "inspect", "--raw", knownListImage)
	manifestDigest, err := manifest.Digest([]byte(m))
	require.NoError(t, err)
	digest := manifestDigest.String()
	list, err := manifest.ListFromBlob([]byte(m), manifest.GuessMIMEType([]byte(m)))
	require.NoError(t, err)
	amd64Instance, err := list.ChooseInstance(&types.SystemContext{ArchitectureChoice: "amd64"})
	require.NoError(t, err)
	arm64Instance, err := list.ChooseInstance(&types.SystemContext{ArchitectureChoice: "arm64"})
	require.NoError(t, err)
	assertSkopeoSucceeds(t, "", "--override-arch=amd64", "copy", knownListImageRepo+"@"+digest, "containers-storage:"+storage+"test@"+digest)
	assertSkopeoSucceeds(t, "", "--override-arch=arm64", "copy", knownListImageRepo+"@"+arm64Instance.String(), "containers-storage:"+storage+"test@"+arm64Instance.String())
	i1 := combinedOutputOfCommand(t, skopeoBinary, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	var image1 imgspecv1.Image
	err = json.Unmarshal([]byte(i1), &image1)
	require.NoError(t, err)
	assert.Equal(t, "amd64", image1.Architecture)
	i2 := combinedOutputOfCommand(t, skopeoBinary, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test@"+amd64Instance.String())
	var image2 imgspecv1.Image
	err = json.Unmarshal([]byte(i2), &image2)
	require.NoError(t, err)
	assert.Equal(t, "amd64", image2.Architecture)
	assertSkopeoFails(t, `.*reading manifest for image instance.*does not exist.*`, "--override-arch=arm64", "inspect", "containers-storage:"+storage+"test@"+digest)
	assertSkopeoFails(t, `.*reading manifest for image instance.*does not exist.*`, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	i3 := combinedOutputOfCommand(t, skopeoBinary, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test@"+arm64Instance.String())
	var image3 imgspecv1.Image
	err = json.Unmarshal([]byte(i3), &image3)
	require.NoError(t, err)
	assert.Equal(t, "arm64", image3.Architecture)
}

func (s *copySuite) TestCopyWithManifestListStorageDigestMultipleArchesSecondUsesListDigest() {
	t := s.T()
	storage := t.TempDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	m := combinedOutputOfCommand(t, skopeoBinary, "inspect", "--raw", knownListImage)
	manifestDigest, err := manifest.Digest([]byte(m))
	require.NoError(t, err)
	digest := manifestDigest.String()
	list, err := manifest.ListFromBlob([]byte(m), manifest.GuessMIMEType([]byte(m)))
	require.NoError(t, err)
	amd64Instance, err := list.ChooseInstance(&types.SystemContext{ArchitectureChoice: "amd64"})
	require.NoError(t, err)
	arm64Instance, err := list.ChooseInstance(&types.SystemContext{ArchitectureChoice: "arm64"})
	require.NoError(t, err)
	assertSkopeoSucceeds(t, "", "--override-arch=amd64", "copy", knownListImageRepo+"@"+amd64Instance.String(), "containers-storage:"+storage+"test@"+amd64Instance.String())
	assertSkopeoSucceeds(t, "", "--override-arch=arm64", "copy", knownListImageRepo+"@"+digest, "containers-storage:"+storage+"test@"+digest)
	i1 := combinedOutputOfCommand(t, skopeoBinary, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test@"+amd64Instance.String())
	var image1 imgspecv1.Image
	err = json.Unmarshal([]byte(i1), &image1)
	require.NoError(t, err)
	assert.Equal(t, "amd64", image1.Architecture)
	assertSkopeoFails(t, `.*reading manifest for image instance.*does not exist.*`, "--override-arch=amd64", "inspect", "containers-storage:"+storage+"test@"+digest)
	assertSkopeoFails(t, `.*reading manifest for image instance.*does not exist.*`, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	i2 := combinedOutputOfCommand(t, skopeoBinary, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	var image2 imgspecv1.Image
	err = json.Unmarshal([]byte(i2), &image2)
	require.NoError(t, err)
	assert.Equal(t, "arm64", image2.Architecture)
	i3 := combinedOutputOfCommand(t, skopeoBinary, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test@"+arm64Instance.String())
	var image3 imgspecv1.Image
	err = json.Unmarshal([]byte(i3), &image3)
	require.NoError(t, err)
	assert.Equal(t, "arm64", image3.Architecture)
}

func (s *copySuite) TestCopyWithManifestListStorageDigestMultipleArchesThirdUsesListDigest() {
	t := s.T()
	storage := t.TempDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	m := combinedOutputOfCommand(t, skopeoBinary, "inspect", "--raw", knownListImage)
	manifestDigest, err := manifest.Digest([]byte(m))
	require.NoError(t, err)
	digest := manifestDigest.String()
	list, err := manifest.ListFromBlob([]byte(m), manifest.GuessMIMEType([]byte(m)))
	require.NoError(t, err)
	amd64Instance, err := list.ChooseInstance(&types.SystemContext{ArchitectureChoice: "amd64"})
	require.NoError(t, err)
	arm64Instance, err := list.ChooseInstance(&types.SystemContext{ArchitectureChoice: "arm64"})
	require.NoError(t, err)
	assertSkopeoSucceeds(t, "", "--override-arch=amd64", "copy", knownListImageRepo+"@"+amd64Instance.String(), "containers-storage:"+storage+"test@"+amd64Instance.String())
	assertSkopeoSucceeds(t, "", "--override-arch=amd64", "copy", knownListImageRepo+"@"+digest, "containers-storage:"+storage+"test@"+digest)
	assertSkopeoSucceeds(t, "", "--override-arch=arm64", "copy", knownListImageRepo+"@"+digest, "containers-storage:"+storage+"test@"+digest)
	assertSkopeoFails(t, `.*reading manifest for image instance.*does not exist.*`, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	i1 := combinedOutputOfCommand(t, skopeoBinary, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test@"+amd64Instance.String())
	var image1 imgspecv1.Image
	err = json.Unmarshal([]byte(i1), &image1)
	require.NoError(t, err)
	assert.Equal(t, "amd64", image1.Architecture)
	i2 := combinedOutputOfCommand(t, skopeoBinary, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	var image2 imgspecv1.Image
	err = json.Unmarshal([]byte(i2), &image2)
	require.NoError(t, err)
	assert.Equal(t, "arm64", image2.Architecture)
	i3 := combinedOutputOfCommand(t, skopeoBinary, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test@"+arm64Instance.String())
	var image3 imgspecv1.Image
	err = json.Unmarshal([]byte(i3), &image3)
	require.NoError(t, err)
	assert.Equal(t, "arm64", image3.Architecture)
}

func (s *copySuite) TestCopyWithManifestListStorageDigestMultipleArchesTagAndDigest() {
	t := s.T()
	storage := t.TempDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	m := combinedOutputOfCommand(t, skopeoBinary, "inspect", "--raw", knownListImage)
	manifestDigest, err := manifest.Digest([]byte(m))
	require.NoError(t, err)
	digest := manifestDigest.String()
	list, err := manifest.ListFromBlob([]byte(m), manifest.GuessMIMEType([]byte(m)))
	require.NoError(t, err)
	amd64Instance, err := list.ChooseInstance(&types.SystemContext{ArchitectureChoice: "amd64"})
	require.NoError(t, err)
	arm64Instance, err := list.ChooseInstance(&types.SystemContext{ArchitectureChoice: "arm64"})
	require.NoError(t, err)
	assertSkopeoSucceeds(t, "", "--override-arch=amd64", "copy", knownListImage, "containers-storage:"+storage+"test:latest")
	assertSkopeoSucceeds(t, "", "--override-arch=arm64", "copy", knownListImageRepo+"@"+digest, "containers-storage:"+storage+"test@"+digest)
	assertSkopeoFails(t, `.*reading manifest for image instance.*does not exist.*`, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	i1 := combinedOutputOfCommand(t, skopeoBinary, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test:latest")
	var image1 imgspecv1.Image
	err = json.Unmarshal([]byte(i1), &image1)
	require.NoError(t, err)
	assert.Equal(t, "amd64", image1.Architecture)
	i2 := combinedOutputOfCommand(t, skopeoBinary, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test@"+amd64Instance.String())
	var image2 imgspecv1.Image
	err = json.Unmarshal([]byte(i2), &image2)
	require.NoError(t, err)
	assert.Equal(t, "amd64", image2.Architecture)
	i3 := combinedOutputOfCommand(t, skopeoBinary, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test:latest")
	var image3 imgspecv1.Image
	err = json.Unmarshal([]byte(i3), &image3)
	require.NoError(t, err)
	assert.Equal(t, "amd64", image3.Architecture)
	i4 := combinedOutputOfCommand(t, skopeoBinary, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test@"+arm64Instance.String())
	var image4 imgspecv1.Image
	err = json.Unmarshal([]byte(i4), &image4)
	require.NoError(t, err)
	assert.Equal(t, "arm64", image4.Architecture)
	i5 := combinedOutputOfCommand(t, skopeoBinary, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	var image5 imgspecv1.Image
	err = json.Unmarshal([]byte(i5), &image5)
	require.NoError(t, err)
	assert.Equal(t, "arm64", image5.Architecture)
}

func (s *copySuite) TestCopyFailsWhenImageOSDoesNotMatchRuntimeOS() {
	t := s.T()
	storage := t.TempDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	assertSkopeoFails(t, `.*no image found in manifest list for architecture .*, variant .*, OS .*`, "copy", knownWindowsOnlyImage, "containers-storage:"+storage+"test")
}

func (s *copySuite) TestCopySucceedsWhenImageDoesNotMatchRuntimeButWeOverride() {
	t := s.T()
	storage := t.TempDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	assertSkopeoSucceeds(t, "", "--override-os=windows", "--override-arch=amd64", "copy", knownWindowsOnlyImage, "--retry-times", "3",
		"containers-storage:"+storage+"test")
}

func (s *copySuite) TestCopySimpleAtomicRegistry() {
	t := s.T()
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	// "pull": docker: → dir:
	assertSkopeoSucceeds(t, "", "copy", testFQIN64, "--retry-times", "3", "dir:"+dir1)
	// "push": dir: → atomic:
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--debug", "copy", "dir:"+dir1, "atomic:localhost:5000/myns/unsigned:unsigned")
	// The result of pushing and pulling is an equivalent image, except for schema1 embedded names.
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "atomic:localhost:5000/myns/unsigned:unsigned", "dir:"+dir2)
	assertSchema1DirImagesAreEqualExceptNames(t, dir1, "libpod/busybox:amd64", dir2, "myns/unsigned:unsigned")
}

// The most basic (skopeo copy) use:
func (s *copySuite) TestCopySimple() {
	t := s.T()
	const ourRegistry = "docker://" + v2DockerRegistryURL + "/"

	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	// "pull": docker: → dir:
	assertSkopeoSucceeds(t, "", "copy", "docker://registry.k8s.io/pause", "dir:"+dir1)
	// "push": dir: → docker(v2s2):
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--debug", "copy", "dir:"+dir1, ourRegistry+"pause:unsigned")
	// The result of pushing and pulling is an unmodified image.
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", ourRegistry+"pause:unsigned", "dir:"+dir2)
	out := combinedOutputOfCommand(t, "diff", "-urN", dir1, dir2)
	assert.Equal(t, "", out)

	// docker v2s2 -> OCI image layout with image name
	// ociDest will be created by oci: if it doesn't exist
	// so don't create it here to exercise auto-creation
	ociDest := "pause-latest-image"
	ociImgName := "pause"
	defer os.RemoveAll(ociDest)
	assertSkopeoSucceeds(t, "", "copy", "docker://registry.k8s.io/pause:latest", "oci:"+ociDest+":"+ociImgName)
	_, err := os.Stat(ociDest)
	require.NoError(t, err)
	// copy exits with status 2 if the image is not found within the container, in some transports.
	assertSkopeoFailsWithStatus(t, 2, "copy", "oci:"+ociDest+":thisdoesnotexist", "dir:"+t.TempDir())

	// docker v2s2 -> OCI image layout without image name
	ociDest = "pause-latest-noimage"
	defer os.RemoveAll(ociDest)
	assertSkopeoSucceeds(t, "", "copy", "docker://registry.k8s.io/pause:latest", "oci:"+ociDest)
	_, err = os.Stat(ociDest)
	require.NoError(t, err)
}

func (s *copySuite) TestCopyEncryption() {
	t := s.T()
	originalImageDir := t.TempDir()
	encryptedImgDir := t.TempDir()
	decryptedImgDir := t.TempDir()
	keysDir := t.TempDir()
	undecryptedImgDir := t.TempDir()
	multiLayerImageDir := t.TempDir()
	partiallyEncryptedImgDir := t.TempDir()
	partiallyDecryptedImgDir := t.TempDir()

	// Create RSA key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	require.NoError(t, err)
	publicKey := &privateKey.PublicKey
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	require.NoError(t, err)
	err = os.WriteFile(keysDir+"/private.key", privateKeyBytes, 0644)
	require.NoError(t, err)
	err = os.WriteFile(keysDir+"/public.key", publicKeyBytes, 0644)
	require.NoError(t, err)

	// We can either perform encryption or decryption on the image.
	// This is why use should not be able to specify both encryption and decryption
	// during copy at the same time.
	assertSkopeoFails(t, ".*--encryption-key and --decryption-key cannot be specified together.*",
		"copy", "--encryption-key", "jwe:"+keysDir+"/public.key", "--decryption-key", keysDir+"/private.key",
		"oci:"+encryptedImgDir+":encrypted", "oci:"+decryptedImgDir+":decrypted")
	assertSkopeoFails(t, ".*--encryption-key and --decryption-key cannot be specified together.*",
		"copy", "--decryption-key", keysDir+"/private.key", "--encryption-key", "jwe:"+keysDir+"/public.key",
		"oci:"+encryptedImgDir+":encrypted", "oci:"+decryptedImgDir+":decrypted")

	// Copy a standard busybox image locally
	assertSkopeoSucceeds(t, "", "copy", "--retry-times", "3",
		testFQIN+":1.30.1", "oci:"+originalImageDir+":latest")

	// Encrypt the image
	assertSkopeoSucceeds(t, "", "copy", "--encryption-key",
		"jwe:"+keysDir+"/public.key", "oci:"+originalImageDir+":latest", "oci:"+encryptedImgDir+":encrypted")

	// An attempt to decrypt an encrypted image without a valid private key should fail
	invalidPrivateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	require.NoError(t, err)
	invalidPrivateKeyBytes := x509.MarshalPKCS1PrivateKey(invalidPrivateKey)
	err = os.WriteFile(keysDir+"/invalid_private.key", invalidPrivateKeyBytes, 0644)
	require.NoError(t, err)
	assertSkopeoFails(t, ".*no suitable key unwrapper found or none of the private keys could be used for decryption.*",
		"copy", "--decryption-key", keysDir+"/invalid_private.key",
		"oci:"+encryptedImgDir+":encrypted", "oci:"+decryptedImgDir+":decrypted")

	// Copy encrypted image without decrypting it
	assertSkopeoSucceeds(t, "", "copy", "oci:"+encryptedImgDir+":encrypted", "oci:"+undecryptedImgDir+":encrypted")
	// Original busybox image has gzipped layers. But encrypted busybox layers should
	// not be of gzip type
	matchLayerBlobBinaryType(t, undecryptedImgDir+"/blobs/sha256", "application/x-gzip", 0)

	// Decrypt the image
	assertSkopeoSucceeds(t, "", "copy", "--decryption-key", keysDir+"/private.key",
		"oci:"+undecryptedImgDir+":encrypted", "oci:"+decryptedImgDir+":decrypted")

	// After successful decryption we should find the gzipped layer from the
	// busybox image
	matchLayerBlobBinaryType(t, decryptedImgDir+"/blobs/sha256", "application/x-gzip", 1)

	// Copy a standard multi layer nginx image locally
	assertSkopeoSucceeds(t, "", "copy", "--retry-times", "3", testFQINMultiLayer, "oci:"+multiLayerImageDir+":latest")

	// Partially encrypt the image
	assertSkopeoSucceeds(t, "", "copy", "--encryption-key", "jwe:"+keysDir+"/public.key",
		"--encrypt-layer", "1", "oci:"+multiLayerImageDir+":latest", "oci:"+partiallyEncryptedImgDir+":encrypted")

	// Since the image is partially encrypted we should find layers that aren't encrypted
	matchLayerBlobBinaryType(t, partiallyEncryptedImgDir+"/blobs/sha256", "application/x-gzip", 2)

	// Decrypt the partially encrypted image
	assertSkopeoSucceeds(t, "", "copy", "--decryption-key", keysDir+"/private.key",
		"oci:"+partiallyEncryptedImgDir+":encrypted", "oci:"+partiallyDecryptedImgDir+":decrypted")

	// After successful decryption we should find the gzipped layers from the nginx image
	matchLayerBlobBinaryType(t, partiallyDecryptedImgDir+"/blobs/sha256", "application/x-gzip", 3)

}

func matchLayerBlobBinaryType(t *testing.T, ociImageDirPath string, contentType string, matchCount int) {
	files, err := os.ReadDir(ociImageDirPath)
	require.NoError(t, err)

	foundCount := 0
	for _, f := range files {
		fileContent, err := os.Open(ociImageDirPath + "/" + f.Name())
		require.NoError(t, err)
		layerContentType, err := getFileContentType(fileContent)
		require.NoError(t, err)

		if layerContentType == contentType {
			foundCount++
		}
	}

	assert.Equal(t, matchCount, foundCount)
}

func getFileContentType(out *os.File) (string, error) {
	buffer := make([]byte, 512)
	if _, err := out.Read(buffer); err != nil {
		return "", err
	}
	contentType := http.DetectContentType(buffer)

	return contentType, nil
}

// Check whether dir: images in dir1 and dir2 are equal, ignoring schema1 signatures.
func assertDirImagesAreEqual(t *testing.T, dir1, dir2 string) {
	// The manifests may have different JWS signatures; so, compare the manifests by digests, which
	// strips the signatures.
	digests := []digest.Digest{}
	for _, dir := range []string{dir1, dir2} {
		manifestPath := filepath.Join(dir, "manifest.json")
		m, err := os.ReadFile(manifestPath)
		require.NoError(t, err)
		digest, err := manifest.Digest(m)
		require.NoError(t, err)
		digests = append(digests, digest)
	}
	assert.Equal(t, digests[1], digests[0])
	// Then compare the rest file by file.
	out := combinedOutputOfCommand(t, "diff", "-urN", "-x", "manifest.json", dir1, dir2)
	assert.Equal(t, "", out)
}

// Check whether schema1 dir: images in dir1 and dir2 are equal, ignoring schema1 signatures and the embedded path/tag values, which should have the expected values.
func assertSchema1DirImagesAreEqualExceptNames(t *testing.T, dir1, ref1, dir2, ref2 string) {
	// The manifests may have different JWS signatures and names; so, unmarshal and delete these elements.
	manifests := []map[string]any{}
	for dir, ref := range map[string]string{dir1: ref1, dir2: ref2} {
		manifestPath := filepath.Join(dir, "manifest.json")
		m, err := os.ReadFile(manifestPath)
		require.NoError(t, err)
		data := map[string]any{}
		err = json.Unmarshal(m, &data)
		require.NoError(t, err)
		assert.Equal(t, float64(1), data["schemaVersion"])
		colon := strings.LastIndex(ref, ":")
		require.NotEqual(t, -1, colon)
		assert.Equal(t, ref[:colon], data["name"])
		assert.Equal(t, ref[colon+1:], data["tag"])
		for _, key := range []string{"signatures", "name", "tag"} {
			delete(data, key)
		}
		manifests = append(manifests, data)
	}
	assert.Equal(t, manifests[0], manifests[1])
	// Then compare the rest file by file.
	out := combinedOutputOfCommand(t, "diff", "-urN", "-x", "manifest.json", dir1, dir2)
	assert.Equal(t, "", out)
}

// Streaming (skopeo copy)
func (s *copySuite) TestCopyStreaming() {
	t := s.T()
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	// streaming: docker: → atomic:
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--debug", "copy", "--retry-times", "3",
		testFQIN64, "atomic:localhost:5000/myns/unsigned:streaming")
	// Compare (copies of) the original and the copy:
	assertSkopeoSucceeds(t, "", "copy", "--retry-times", "3", testFQIN64, "dir:"+dir1)
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "atomic:localhost:5000/myns/unsigned:streaming", "dir:"+dir2)
	assertSchema1DirImagesAreEqualExceptNames(t, dir1, "libpod/busybox:amd64", dir2, "myns/unsigned:streaming")
	// FIXME: Also check pushing to docker://
}

// OCI round-trip testing. It's very important to make sure that OCI <-> Docker
// conversion works (while skopeo handles many things, one of the most obvious
// benefits of a tool like skopeo is that you can use OCI tooling to create an
// image and then as the final step convert the image to a non-standard format
// like Docker). But this only works if we _test_ it.
func (s *copySuite) TestCopyOCIRoundTrip() {
	t := s.T()
	const ourRegistry = "docker://" + v2DockerRegistryURL + "/"

	oci1 := t.TempDir()
	oci2 := t.TempDir()

	// Docker -> OCI
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--debug", "copy", "--retry-times", "3",
		testFQIN, "oci:"+oci1+":latest")
	// OCI -> Docker
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--debug", "copy", "oci:"+oci1+":latest", ourRegistry+"original/busybox:oci_copy")
	// Docker -> OCI
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--debug", "copy", ourRegistry+"original/busybox:oci_copy", "oci:"+oci2+":latest")
	// OCI -> Docker
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--debug", "copy", "oci:"+oci2+":latest", ourRegistry+"original/busybox:oci_copy2")

	// TODO: Add some more tags to output to and check those work properly.

	// First, make sure the OCI blobs are the same. This should _always_ be true.
	out := combinedOutputOfCommand(t, "diff", "-urN", oci1+"/blobs", oci2+"/blobs")
	assert.Equal(t, "", out)

	// For some silly reason we pass a logger to the OCI library here...
	logger := log.New(os.Stderr, "", 0)

	// Verify using the upstream OCI image validator, this should catch most
	// non-compliance errors. DO NOT REMOVE THIS TEST UNLESS IT'S ABSOLUTELY
	// NECESSARY.
	err := image.ValidateLayout(oci1, nil, logger)
	require.NoError(t, err)
	err = image.ValidateLayout(oci2, nil, logger)
	require.NoError(t, err)

	// Now verify that everything is identical. Currently this is true, but
	// because we recompute the manifests on-the-fly this doesn't necessarily
	// always have to be true (but if this breaks in the future __PLEASE__ make
	// sure that the breakage actually makes sense before removing this check).
	out = combinedOutputOfCommand(t, "diff", "-urN", oci1, oci2)
	assert.Equal(t, "", out)
}

// --sign-by and --policy copy, primarily using atomic:
func (s *copySuite) TestCopySignatures() {
	t := s.T()
	mech, _, err := signature.NewEphemeralGPGSigningMechanism([]byte{})
	require.NoError(t, err)
	defer mech.Close()
	if err := mech.SupportsSigning(); err != nil { // FIXME? Test that verification and policy enforcement works, using signatures from fixtures
		t.Skipf("Signing not supported: %v", err)
	}

	dir := t.TempDir()
	dirDest := "dir:" + dir

	policy := fileFromFixture(t, "fixtures/policy.json", map[string]string{"@keydir@": s.gpgHome})
	defer os.Remove(policy)

	// type: reject
	assertSkopeoFails(t, fmt.Sprintf(".*Source image rejected: Running image %s:latest is rejected by policy.*", testFQIN),
		"--policy", policy, "copy", testFQIN+":latest", dirDest)

	// type: insecureAcceptAnything
	assertSkopeoSucceeds(t, "", "--policy", policy, "copy", "docker://quay.io/openshift/origin-hello-openshift", dirDest)

	// type: signedBy
	// Sign the images
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "--retry-times", "3", "--sign-by", "personal@example.com",
		testFQIN+":1.26", "atomic:localhost:5006/myns/personal:personal")
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "--retry-times", "3", "--sign-by", "official@example.com",
		testFQIN+":1.26.1", "atomic:localhost:5006/myns/official:official")
	// Verify that we can pull them
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--policy", policy, "copy", "atomic:localhost:5006/myns/personal:personal", dirDest)
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--policy", policy, "copy", "atomic:localhost:5006/myns/official:official", dirDest)
	// Verify that mis-signed images are rejected
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "atomic:localhost:5006/myns/personal:personal", "atomic:localhost:5006/myns/official:attack")
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "atomic:localhost:5006/myns/official:official", "atomic:localhost:5006/myns/personal:attack")
	assertSkopeoFails(t, ".*Source image rejected: Invalid GPG signature.*",
		"--tls-verify=false", "--policy", policy, "copy", "atomic:localhost:5006/myns/personal:attack", dirDest)
	assertSkopeoFails(t, ".*Source image rejected: Invalid GPG signature.*",
		"--tls-verify=false", "--policy", policy, "copy", "atomic:localhost:5006/myns/official:attack", dirDest)

	// Verify that signed identity is verified.
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "atomic:localhost:5006/myns/official:official", "atomic:localhost:5006/myns/naming:test1")
	assertSkopeoFails(t, `.*Source image rejected: Signature for identity \\"localhost:5006/myns/official:official\\" is not accepted.*`,
		"--tls-verify=false", "--policy", policy, "copy", "atomic:localhost:5006/myns/naming:test1", dirDest)
	// signedIdentity works
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "atomic:localhost:5006/myns/official:official", "atomic:localhost:5006/myns/naming:naming")
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--policy", policy, "copy", "atomic:localhost:5006/myns/naming:naming", dirDest)

	// Verify that cosigning requirements are enforced
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "atomic:localhost:5006/myns/official:official", "atomic:localhost:5006/myns/cosigned:cosigned")
	assertSkopeoFails(t, ".*Source image rejected: Invalid GPG signature.*",
		"--tls-verify=false", "--policy", policy, "copy", "atomic:localhost:5006/myns/cosigned:cosigned", dirDest)

	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "--sign-by", "personal@example.com", "atomic:localhost:5006/myns/official:official", "atomic:localhost:5006/myns/cosigned:cosigned")
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--policy", policy, "copy", "atomic:localhost:5006/myns/cosigned:cosigned", dirDest)
}

// --policy copy for dir: sources
func (s *copySuite) TestCopyDirSignatures() {
	t := s.T()
	mech, _, err := signature.NewEphemeralGPGSigningMechanism([]byte{})
	require.NoError(t, err)
	defer mech.Close()
	if err := mech.SupportsSigning(); err != nil { // FIXME? Test that verification and policy enforcement works, using signatures from fixtures
		t.Skipf("Signing not supported: %v", err)
	}

	topDir := t.TempDir()
	topDirDest := "dir:" + topDir

	for _, suffix := range []string{"/dir1", "/dir2", "/restricted/personal", "/restricted/official", "/restricted/badidentity", "/dest"} {
		err := os.MkdirAll(topDir+suffix, 0755)
		require.NoError(t, err)
	}

	// Note the "/@dirpath@": The value starts with a slash so that it is not rejected in other tests which do not replace it,
	// but we must ensure that the result is a canonical path, not something starting with a "//".
	policy := fileFromFixture(t, "fixtures/policy.json", map[string]string{"@keydir@": s.gpgHome, "/@dirpath@": topDir + "/restricted"})
	defer os.Remove(policy)

	// Get some images.
	assertSkopeoSucceeds(t, "", "copy", "--retry-times", "3", testFQIN+":armfh", topDirDest+"/dir1")
	assertSkopeoSucceeds(t, "", "copy", "--retry-times", "3", testFQIN+":s390x", topDirDest+"/dir2")

	// Sign the images. By coping from a topDirDest/dirN, also test that non-/restricted paths
	// use the dir:"" default of insecureAcceptAnything.
	// (For signing, we must push to atomic: to get a Docker identity to use in the signature.)
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--policy", policy, "copy", "--sign-by", "personal@example.com", topDirDest+"/dir1", "atomic:localhost:5000/myns/personal:dirstaging")
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--policy", policy, "copy", "--sign-by", "official@example.com", topDirDest+"/dir2", "atomic:localhost:5000/myns/official:dirstaging")
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "atomic:localhost:5000/myns/personal:dirstaging", topDirDest+"/restricted/personal")
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "atomic:localhost:5000/myns/official:dirstaging", topDirDest+"/restricted/official")

	// type: signedBy, with a signedIdentity override (necessary because dir: identities can't be signed)
	// Verify that correct images are accepted
	assertSkopeoSucceeds(t, "", "--policy", policy, "copy", topDirDest+"/restricted/official", topDirDest+"/dest")
	// ... and that mis-signed images are rejected.
	assertSkopeoFails(t, ".*Source image rejected: Invalid GPG signature.*",
		"--policy", policy, "copy", topDirDest+"/restricted/personal", topDirDest+"/dest")

	// Verify that the signed identity is verified.
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--policy", policy, "copy", "--sign-by", "official@example.com", topDirDest+"/dir1", "atomic:localhost:5000/myns/personal:dirstaging2")
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "atomic:localhost:5000/myns/personal:dirstaging2", topDirDest+"/restricted/badidentity")
	assertSkopeoFails(t, `.*Source image rejected: .*Signature for identity \\"localhost:5000/myns/personal:dirstaging2\\" is not accepted.*`,
		"--policy", policy, "copy", topDirDest+"/restricted/badidentity", topDirDest+"/dest")
}

// Compression during copy
func (s *copySuite) TestCopyCompression() {
	t := s.T()
	const uncompresssedLayerFile = "160d823fdc48e62f97ba62df31e55424f8f5eb6b679c865eec6e59adfe304710"

	topDir := t.TempDir()

	for i, c := range []struct{ fixture, remote string }{
		{"uncompressed-image-s1", "docker://" + v2DockerRegistryURL + "/compression/compression:s1"},
		{"uncompressed-image-s2", "docker://" + v2DockerRegistryURL + "/compression/compression:s2"},
		{"uncompressed-image-s1", "atomic:localhost:5000/myns/compression:s1"},
		{"uncompressed-image-s2", "atomic:localhost:5000/myns/compression:s2"},
	} {
		dir := filepath.Join(topDir, fmt.Sprintf("case%d", i))
		err := os.MkdirAll(dir, 0755)
		require.NoError(t, err)

		assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "dir:fixtures/"+c.fixture, c.remote)
		assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", c.remote, "dir:"+dir)

		// The original directory contained an uncompressed file, the copy after pushing and pulling doesn't (we use a different name for the compressed file).
		_, err = os.Lstat(filepath.Join("fixtures", c.fixture, uncompresssedLayerFile))
		require.NoError(t, err)
		_, err = os.Lstat(filepath.Join(dir, uncompresssedLayerFile))
		require.Error(t, err)
		assert.True(t, os.IsNotExist(err))

		// All pulled layers are smaller than the uncompressed size of uncompresssedLayerFile. (Note that this includes the manifest in s2, but that works out OK).
		dirf, err := os.Open(dir)
		require.NoError(t, err)
		fis, err := dirf.Readdir(-1)
		require.NoError(t, err)
		for _, fi := range fis {
			assert.Less(t, fi.Size(), int64(2048))
		}
	}
}

func findRegularFiles(t *testing.T, root string) []string {
	result := []string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			result = append(result, path)
		}
		return nil
	})
	require.NoError(t, err)
	return result
}

// --sign-by and policy use for docker: with lookaside
func (s *copySuite) TestCopyDockerLookaside() {
	t := s.T()
	mech, _, err := signature.NewEphemeralGPGSigningMechanism([]byte{})
	require.NoError(t, err)
	defer mech.Close()
	if err := mech.SupportsSigning(); err != nil { // FIXME? Test that verification and policy enforcement works, using signatures from fixtures
		t.Skipf("Signing not supported: %v", err)
	}

	const ourRegistry = "docker://" + v2DockerRegistryURL + "/"

	tmpDir := t.TempDir()
	copyDest := filepath.Join(tmpDir, "dest")
	err = os.Mkdir(copyDest, 0755)
	require.NoError(t, err)
	dirDest := "dir:" + copyDest
	plainLookaside := filepath.Join(tmpDir, "lookaside")
	splitLookasideStaging := filepath.Join(tmpDir, "lookaside-staging")

	splitLookasideReadServerHandler := http.NotFoundHandler()
	splitLookasideReadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		splitLookasideReadServerHandler.ServeHTTP(w, r)
	}))
	defer splitLookasideReadServer.Close()

	policy := fileFromFixture(t, "fixtures/policy.json", map[string]string{"@keydir@": s.gpgHome})
	defer os.Remove(policy)
	registriesDir := filepath.Join(tmpDir, "registries.d")
	err = os.Mkdir(registriesDir, 0755)
	require.NoError(t, err)
	registriesFile := fileFromFixture(t, "fixtures/registries.yaml",
		map[string]string{"@lookaside@": plainLookaside, "@split-staging@": splitLookasideStaging, "@split-read@": splitLookasideReadServer.URL})
	err = os.Symlink(registriesFile, filepath.Join(registriesDir, "registries.yaml"))
	require.NoError(t, err)

	// Get an image to work with.  Also verifies that we can use Docker repositories with no lookaside configured.
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--registries.d", registriesDir, "copy", "--retry-times", "3",
		testFQIN, ourRegistry+"original/busybox")
	// Pulling an unsigned image fails.
	assertSkopeoFails(t, ".*Source image rejected: A signature was required, but no signature exists.*",
		"--tls-verify=false", "--policy", policy, "--registries.d", registriesDir, "copy", ourRegistry+"original/busybox", dirDest)

	// Signing with lookaside defined succeeds,
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--registries.d", registriesDir, "copy", "--sign-by", "personal@example.com", ourRegistry+"original/busybox", ourRegistry+"signed/busybox")
	// a signature file has been created,
	foundFiles := findRegularFiles(t, plainLookaside)
	assert.Len(t, foundFiles, 1)
	// and pulling a signed image succeeds.
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--policy", policy, "--registries.d", registriesDir, "copy", ourRegistry+"signed/busybox", dirDest)

	// Deleting the image succeeds,
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--registries.d", registriesDir, "delete", ourRegistry+"signed/busybox")
	// and the signature file has been deleted (but we leave the directories around).
	foundFiles = findRegularFiles(t, plainLookaside)
	assert.Len(t, foundFiles, 0)

	// Signing with a read/write lookaside split succeeds,
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--registries.d", registriesDir, "copy", "--sign-by", "personal@example.com", ourRegistry+"original/busybox", ourRegistry+"public/busybox")
	// and a signature file has been created.
	foundFiles = findRegularFiles(t, splitLookasideStaging)
	assert.Len(t, foundFiles, 1)
	// Pulling the image fails because the read lookaside URL has not been populated:
	assertSkopeoFails(t, ".*Source image rejected: A signature was required, but no signature exists.*",
		"--tls-verify=false", "--policy", policy, "--registries.d", registriesDir, "copy", ourRegistry+"public/busybox", dirDest)
	// Pulling the image succeeds after the read lookaside URL is available:
	splitLookasideReadServerHandler = http.FileServer(http.Dir(splitLookasideStaging))
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--policy", policy, "--registries.d", registriesDir, "copy", ourRegistry+"public/busybox", dirDest)
}

// atomic: and docker: X-Registry-Supports-Signatures works and interoperates
func (s *copySuite) TestCopyAtomicExtension() {
	t := s.T()
	mech, _, err := signature.NewEphemeralGPGSigningMechanism([]byte{})
	require.NoError(t, err)
	defer mech.Close()
	if err := mech.SupportsSigning(); err != nil { // FIXME? Test that the reading/writing works using signatures from fixtures
		t.Skipf("Signing not supported: %v", err)
	}

	topDir := t.TempDir()
	for _, subdir := range []string{"dirAA", "dirAD", "dirDA", "dirDD", "registries.d"} {
		err := os.MkdirAll(filepath.Join(topDir, subdir), 0755)
		require.NoError(t, err)
	}
	registriesDir := filepath.Join(topDir, "registries.d")
	dirDest := "dir:" + topDir
	policy := fileFromFixture(t, "fixtures/policy.json", map[string]string{"@keydir@": s.gpgHome})
	defer os.Remove(policy)

	// Get an image to work with to an atomic: destination.  Also verifies that we can use Docker repositories without X-Registry-Supports-Signatures
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--registries.d", registriesDir, "copy", "--retry-times", "3",
		testFQIN, "atomic:localhost:5000/myns/extension:unsigned")
	// Pulling an unsigned image using atomic: fails.
	assertSkopeoFails(t, ".*Source image rejected: A signature was required, but no signature exists.*",
		"--tls-verify=false", "--policy", policy,
		"copy", "atomic:localhost:5000/myns/extension:unsigned", dirDest+"/dirAA")
	// The same when pulling using docker:
	assertSkopeoFails(t, ".*Source image rejected: A signature was required, but no signature exists.*",
		"--tls-verify=false", "--policy", policy, "--registries.d", registriesDir,
		"copy", "docker://localhost:5000/myns/extension:unsigned", dirDest+"/dirAD")

	// Sign the image using atomic:
	assertSkopeoSucceeds(t, "", "--tls-verify=false",
		"copy", "--sign-by", "personal@example.com", "atomic:localhost:5000/myns/extension:unsigned", "atomic:localhost:5000/myns/extension:atomic")
	// Pulling the image using atomic: now succeeds.
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--policy", policy,
		"copy", "atomic:localhost:5000/myns/extension:atomic", dirDest+"/dirAA")
	// The same when pulling using docker:
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--policy", policy, "--registries.d", registriesDir,
		"copy", "docker://localhost:5000/myns/extension:atomic", dirDest+"/dirAD")
	// Both access methods result in the same data.
	assertDirImagesAreEqual(t, filepath.Join(topDir, "dirAA"), filepath.Join(topDir, "dirAD"))

	// Get another image (different so that they don't share signatures, and sign it using docker://)
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "--registries.d", registriesDir,
		"copy", "--retry-times", "3", "--sign-by", "personal@example.com",
		testFQIN+":ppc64le", "docker://localhost:5000/myns/extension:extension")
	t.Logf("%s", combinedOutputOfCommand(t, "oc", "get", "istag", "extension:extension", "-o", "json"))
	// Pulling the image using atomic: succeeds.
	assertSkopeoSucceeds(t, "", "--debug", "--tls-verify=false", "--policy", policy,
		"copy", "atomic:localhost:5000/myns/extension:extension", dirDest+"/dirDA")
	// The same when pulling using docker:
	assertSkopeoSucceeds(t, "", "--debug", "--tls-verify=false", "--policy", policy, "--registries.d", registriesDir,
		"copy", "docker://localhost:5000/myns/extension:extension", dirDest+"/dirDD")
	// Both access methods result in the same data.
	assertDirImagesAreEqual(t, filepath.Join(topDir, "dirDA"), filepath.Join(topDir, "dirDD"))
}

// Both mirroring support in registries.conf, and mirrored remapIdentity support in policy.json
func (s *copySuite) TestCopyVerifyingMirroredSignatures() {
	t := s.T()
	const regPrefix = "docker://localhost:5006/myns/mirroring-"

	mech, _, err := signature.NewEphemeralGPGSigningMechanism([]byte{})
	require.NoError(t, err)
	defer mech.Close()
	if err := mech.SupportsSigning(); err != nil { // FIXME? Test that verification and policy enforcement works, using signatures from fixtures
		t.Skipf("Signing not supported: %v", err)
	}

	topDir := t.TempDir()
	registriesDir := filepath.Join(topDir, "registries.d") // An empty directory to disable lookaside use
	dirDest := "dir:" + filepath.Join(topDir, "unused-dest")

	policy := fileFromFixture(t, "fixtures/policy.json", map[string]string{"@keydir@": s.gpgHome})
	defer os.Remove(policy)

	// We use X-R-S-S for this testing to avoid having to deal with the lookasides.
	// A downside is that OpenShift records signatures per image, so the error messages below
	// list all signatures for other tags used for the same image as well.
	// So, make sure to never create a signature that could be considered valid in a different part of the test (i.e. don't reuse tags).

	// Get an image to work with.
	assertSkopeoSucceeds(t, "", "copy", "--retry-times", "3", "--dest-tls-verify=false",
		testFQIN, regPrefix+"primary:unsigned")
	// Verify that unsigned images are rejected
	assertSkopeoFails(t, ".*Source image rejected: A signature was required, but no signature exists.*",
		"--policy", policy, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"primary:unsigned", dirDest)
	// Sign the image for the primary location
	assertSkopeoSucceeds(t, "", "--registries.d", registriesDir, "copy", "--src-tls-verify=false", "--dest-tls-verify=false", "--sign-by", "personal@example.com", regPrefix+"primary:unsigned", regPrefix+"primary:direct")
	// Verify that a correctly signed image in the primary location is usable.
	assertSkopeoSucceeds(t, "", "--policy", policy, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"primary:direct", dirDest)

	// Sign the image for the mirror
	assertSkopeoSucceeds(t, "", "--registries.d", registriesDir, "copy", "--src-tls-verify=false", "--dest-tls-verify=false", "--sign-by", "personal@example.com", regPrefix+"primary:unsigned", regPrefix+"mirror:mirror-signed")
	// Verify that a correctly signed image for the mirror is accessible using the mirror's reference
	assertSkopeoSucceeds(t, "", "--policy", policy, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"mirror:mirror-signed", dirDest)
	// … but verify that while it is accessible using the primary location redirecting to the mirror, …
	assertSkopeoSucceeds(t, "" /* no --policy */, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"primary:mirror-signed", dirDest)
	// … verify it is NOT accessible when requiring a signature.
	assertSkopeoFails(t, `.*Source image rejected: None of the signatures were accepted, reasons: Signature for identity \\"localhost:5006/myns/mirroring-primary:direct\\" is not accepted; Signature for identity \\"localhost:5006/myns/mirroring-mirror:mirror-signed\\" is not accepted.*`,
		"--policy", policy, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"primary:mirror-signed", dirDest)

	// Fail if we specify an unqualified identity
	assertSkopeoFails(t, ".*Could not parse --sign-identity: repository name must be canonical.*",
		"--registries.d", registriesDir, "copy", "--src-tls-verify=false", "--dest-tls-verify=false", "--sign-by=personal@example.com", "--sign-identity=this-is-not-fully-specified", regPrefix+"primary:unsigned", regPrefix+"mirror:primary-signed")

	// Create a signature for mirroring-primary:primary-signed without pushing there.
	assertSkopeoSucceeds(t, "", "--registries.d", registriesDir, "copy", "--src-tls-verify=false", "--dest-tls-verify=false", "--sign-by=personal@example.com", "--sign-identity=localhost:5006/myns/mirroring-primary:primary-signed", regPrefix+"primary:unsigned", regPrefix+"mirror:primary-signed")
	// Verify that a correctly signed image for the primary is accessible using the primary's reference
	assertSkopeoSucceeds(t, "", "--policy", policy, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"primary:primary-signed", dirDest)
	// … but verify that while it is accessible using the mirror location
	assertSkopeoSucceeds(t, "" /* no --policy */, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"mirror:primary-signed", dirDest)
	// … verify it is NOT accessible when requiring a signature.
	assertSkopeoFails(t, `.*Source image rejected: None of the signatures were accepted, reasons: Signature for identity \\"localhost:5006/myns/mirroring-primary:direct\\" is not accepted; Signature for identity \\"localhost:5006/myns/mirroring-mirror:mirror-signed\\" is not accepted; Signature for identity \\"localhost:5006/myns/mirroring-primary:primary-signed\\" is not accepted.*`,
		"--policy", policy, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"mirror:primary-signed", dirDest)

	assertSkopeoSucceeds(t, "", "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", "--dest-tls-verify=false", regPrefix+"primary:unsigned", regPrefix+"remap:remapped")
	// Verify that while a remapIdentity image is accessible using the remapped (mirror) location
	assertSkopeoSucceeds(t, "" /* no --policy */, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"remap:remapped", dirDest)
	// … it is NOT accessible when requiring a signature …
	assertSkopeoFails(t, `.*Source image rejected: None of the signatures were accepted, reasons: Signature for identity \\"localhost:5006/myns/mirroring-primary:direct\\" is not accepted; Signature for identity \\"localhost:5006/myns/mirroring-mirror:mirror-signed\\" is not accepted; Signature for identity \\"localhost:5006/myns/mirroring-primary:primary-signed\\" is not accepted.*`, "--policy", policy, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"remap:remapped", dirDest)
	// … until signed.
	assertSkopeoSucceeds(t, "", "--registries.d", registriesDir, "copy", "--src-tls-verify=false", "--dest-tls-verify=false", "--sign-by=personal@example.com", "--sign-identity=localhost:5006/myns/mirroring-primary:remapped", regPrefix+"remap:remapped", regPrefix+"remap:remapped")
	assertSkopeoSucceeds(t, "", "--policy", policy, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"remap:remapped", dirDest)
	// To be extra clear about the semantics, verify that the signedPrefix (primary) location never exists
	// and only the remapped prefix (mirror) is accessed.
	assertSkopeoFails(t, ".*initializing source docker://localhost:5006/myns/mirroring-primary:remapped:.*manifest unknown.*",
		"--policy", policy, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"primary:remapped", dirDest)
}

func (s *skopeoSuite) TestCopySrcWithAuth() {
	t := s.T()
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "--retry-times", "3", "--dest-creds=testuser:testpassword",
		testFQIN, fmt.Sprintf("docker://%s/busybox:latest", s.regV2WithAuth.url))
	dir1 := t.TempDir()
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "--src-creds=testuser:testpassword", fmt.Sprintf("docker://%s/busybox:latest", s.regV2WithAuth.url), "dir:"+dir1)
}

func (s *skopeoSuite) TestCopyDestWithAuth() {
	t := s.T()
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "--retry-times", "3", "--dest-creds=testuser:testpassword",
		testFQIN, fmt.Sprintf("docker://%s/busybox:latest", s.regV2WithAuth.url))
}

func (s *skopeoSuite) TestCopySrcAndDestWithAuth() {
	t := s.T()
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "--retry-times", "3", "--dest-creds=testuser:testpassword",
		testFQIN, fmt.Sprintf("docker://%s/busybox:latest", s.regV2WithAuth.url))
	assertSkopeoSucceeds(t, "", "--tls-verify=false", "copy", "--src-creds=testuser:testpassword", "--dest-creds=testuser:testpassword", fmt.Sprintf("docker://%s/busybox:latest", s.regV2WithAuth.url), fmt.Sprintf("docker://%s/test:auth", s.regV2WithAuth.url))
}

func (s *copySuite) TestCopyNoPanicOnHTTPResponseWithoutTLSVerifyFalse() {
	t := s.T()
	topDir := t.TempDir()

	const ourRegistry = "docker://" + v2DockerRegistryURL + "/"

	assertSkopeoFails(t, ".*server gave HTTP response to HTTPS client.*",
		"copy", ourRegistry+"foobar", "dir:"+topDir)
}

func (s *copySuite) TestCopySchemaConversion() {
	t := s.T()
	// Test conversion / schema autodetection both for the OpenShift embedded registry…
	s.testCopySchemaConversionRegistries(t, "docker://localhost:5005/myns/schema1", "docker://localhost:5006/myns/schema2")
	// … and for various docker/distribution registry versions.
	s.testCopySchemaConversionRegistries(t, "docker://"+v2s1DockerRegistryURL+"/schema1", "docker://"+v2DockerRegistryURL+"/schema2")
}

func (s *copySuite) TestCopyManifestConversion() {
	t := s.T()
	topDir := t.TempDir()
	srcDir := filepath.Join(topDir, "source")
	destDir1 := filepath.Join(topDir, "dest1")
	destDir2 := filepath.Join(topDir, "dest2")

	// oci to v2s1 and vice-versa not supported yet
	// get v2s2 manifest type
	assertSkopeoSucceeds(t, "", "copy", "--retry-times", "3", testFQIN, "dir:"+srcDir)
	verifyManifestMIMEType(t, srcDir, manifest.DockerV2Schema2MediaType)
	// convert from v2s2 to oci
	assertSkopeoSucceeds(t, "", "copy", "--format=oci", "dir:"+srcDir, "dir:"+destDir1)
	verifyManifestMIMEType(t, destDir1, imgspecv1.MediaTypeImageManifest)
	// convert from oci to v2s2
	assertSkopeoSucceeds(t, "", "copy", "--format=v2s2", "dir:"+destDir1, "dir:"+destDir2)
	verifyManifestMIMEType(t, destDir2, manifest.DockerV2Schema2MediaType)
	// convert from v2s2 to v2s1
	assertSkopeoSucceeds(t, "", "copy", "--format=v2s1", "dir:"+srcDir, "dir:"+destDir1)
	verifyManifestMIMEType(t, destDir1, manifest.DockerV2Schema1SignedMediaType)
	// convert from v2s1 to v2s2
	assertSkopeoSucceeds(t, "", "copy", "--format=v2s2", "dir:"+destDir1, "dir:"+destDir2)
	verifyManifestMIMEType(t, destDir2, manifest.DockerV2Schema2MediaType)
}

func (s *copySuite) TestCopyPreserveDigests() {
	t := s.T()
	topDir := t.TempDir()

	assertSkopeoSucceeds(t, "", "copy", knownListImage, "--multi-arch=all", "--preserve-digests", "dir:"+topDir)
	assertSkopeoFails(t, ".*Instructed to preserve digests.*", "copy", knownListImage, "--multi-arch=all", "--preserve-digests", "--format=oci", "dir:"+topDir)
}

func (s *copySuite) testCopySchemaConversionRegistries(t *testing.T, schema1Registry, schema2Registry string) {
	topDir := t.TempDir()
	for _, subdir := range []string{"input1", "input2", "dest2"} {
		err := os.MkdirAll(filepath.Join(topDir, subdir), 0755)
		require.NoError(t, err)
	}
	input1Dir := filepath.Join(topDir, "input1")
	input2Dir := filepath.Join(topDir, "input2")
	destDir := filepath.Join(topDir, "dest2")

	// Ensure we are working with a schema2 image.
	// dir: accepts any manifest format, i.e. this makes …/input2 a schema2 source which cannot be asked to produce schema1 like ordinary docker: registries can.
	assertSkopeoSucceeds(t, "", "copy", "--retry-times", "3", testFQIN, "dir:"+input2Dir)
	verifyManifestMIMEType(t, input2Dir, manifest.DockerV2Schema2MediaType)
	// 2→2 (the "f2t2" in tag means "from 2 to 2")
	assertSkopeoSucceeds(t, "", "copy", "--dest-tls-verify=false", "dir:"+input2Dir, schema2Registry+":f2t2")
	assertSkopeoSucceeds(t, "", "copy", "--src-tls-verify=false", schema2Registry+":f2t2", "dir:"+destDir)
	verifyManifestMIMEType(t, destDir, manifest.DockerV2Schema2MediaType)
	// 2→1; we will use the result as a schema1 image for further tests.
	assertSkopeoSucceeds(t, "", "copy", "--dest-tls-verify=false", "dir:"+input2Dir, schema1Registry+":f2t1")
	assertSkopeoSucceeds(t, "", "copy", "--src-tls-verify=false", schema1Registry+":f2t1", "dir:"+input1Dir)
	verifyManifestMIMEType(t, input1Dir, manifest.DockerV2Schema1SignedMediaType)
	// 1→1
	assertSkopeoSucceeds(t, "", "copy", "--dest-tls-verify=false", "dir:"+input1Dir, schema1Registry+":f1t1")
	assertSkopeoSucceeds(t, "", "copy", "--src-tls-verify=false", schema1Registry+":f1t1", "dir:"+destDir)
	verifyManifestMIMEType(t, destDir, manifest.DockerV2Schema1SignedMediaType)
	// 1→2: image stays unmodified schema1
	assertSkopeoSucceeds(t, "", "copy", "--dest-tls-verify=false", "dir:"+input1Dir, schema2Registry+":f1t2")
	assertSkopeoSucceeds(t, "", "copy", "--src-tls-verify=false", schema2Registry+":f1t2", "dir:"+destDir)
	verifyManifestMIMEType(t, destDir, manifest.DockerV2Schema1SignedMediaType)
}

const regConfFixture = "./fixtures/registries.conf"

func (s *skopeoSuite) TestSuccessCopySrcWithMirror() {
	t := s.T()
	dir := t.TempDir()

	assertSkopeoSucceeds(t, "", "--registries-conf="+regConfFixture, "copy",
		"docker://mirror.invalid/busybox", "dir:"+dir)
}

func (s *skopeoSuite) TestFailureCopySrcWithMirrorsUnavailable() {
	t := s.T()
	dir := t.TempDir()

	// .invalid domains are, per RFC 6761, supposed to result in NXDOMAIN.
	// With systemd-resolved (used only via NSS?), we instead seem to get “Temporary failure in name resolution”
	assertSkopeoFails(t, ".*(no such host|Temporary failure in name resolution).*",
		"--registries-conf="+regConfFixture, "copy", "docker://invalid.invalid/busybox", "dir:"+dir)
}

func (s *skopeoSuite) TestSuccessCopySrcWithMirrorAndPrefix() {
	t := s.T()
	dir := t.TempDir()

	assertSkopeoSucceeds(t, "", "--registries-conf="+regConfFixture, "copy",
		"docker://gcr.invalid/foo/bar/busybox", "dir:"+dir)
}

func (s *skopeoSuite) TestFailureCopySrcWithMirrorAndPrefixUnavailable() {
	t := s.T()
	dir := t.TempDir()

	// .invalid domains are, per RFC 6761, supposed to result in NXDOMAIN.
	// With systemd-resolved (used only via NSS?), we instead seem to get “Temporary failure in name resolution”
	assertSkopeoFails(t, ".*(no such host|Temporary failure in name resolution).*",
		"--registries-conf="+regConfFixture, "copy", "docker://gcr.invalid/wrong/prefix/busybox", "dir:"+dir)
}

func (s *copySuite) TestCopyFailsWhenReferenceIsInvalid() {
	t := s.T()
	assertSkopeoFails(t, `.*Invalid image name.*`, "copy", "unknown:transport", "unknown:test")
}
