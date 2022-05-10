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

	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/signature"
	"github.com/containers/image/v5/types"
	digest "github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/opencontainers/image-tools/image"
	"gopkg.in/check.v1"
)

func init() {
	check.Suite(&CopySuite{})
}

const (
	v2DockerRegistryURL   = "localhost:8888" // Update also policy.json, registries.yaml
	v2s1DockerRegistryURL = "localhost:8889"
	knownWindowsOnlyImage = "docker://mcr.microsoft.com/windows/nanoserver:1909"
	knownListImage        = "docker://registry.fedoraproject.org/fedora-minimal" // could have either ":latest" or "@sha256:..." appended
)

type CopySuite struct {
	cluster    *openshiftCluster
	registry   *testRegistryV2
	s1Registry *testRegistryV2
	gpgHome    string
}

func (s *CopySuite) SetUpSuite(c *check.C) {
	if os.Getenv("SKOPEO_CONTAINER_TESTS") != "1" {
		c.Skip("Not running in a container, refusing to affect user state")
	}

	s.cluster = startOpenshiftCluster(c) // FIXME: Set up TLS for the docker registry port instead of using "--tls-verify=false" all over the place.

	for _, stream := range []string{"unsigned", "personal", "official", "naming", "cosigned", "compression", "schema1", "schema2"} {
		isJSON := fmt.Sprintf(`{
			"kind": "ImageStream",
			"apiVersion": "v1",
			"metadata": {
			    "name": "%s"
			},
			"spec": {}
		}`, stream)
		runCommandWithInput(c, isJSON, "oc", "create", "-f", "-")
	}

	// FIXME: Set up TLS for the docker registry port instead of using "--tls-verify=false" all over the place.
	s.registry = setupRegistryV2At(c, v2DockerRegistryURL, false, false)
	s.s1Registry = setupRegistryV2At(c, v2s1DockerRegistryURL, false, true)

	s.gpgHome = c.MkDir()
	os.Setenv("GNUPGHOME", s.gpgHome)

	for _, key := range []string{"personal", "official"} {
		batchInput := fmt.Sprintf("Key-Type: RSA\nName-Real: Test key - %s\nName-email: %s@example.com\n%%no-protection\n%%commit\n",
			key, key)
		runCommandWithInput(c, batchInput, gpgBinary, "--batch", "--gen-key")

		out := combinedOutputOfCommand(c, gpgBinary, "--armor", "--export", fmt.Sprintf("%s@example.com", key))
		err := os.WriteFile(filepath.Join(s.gpgHome, fmt.Sprintf("%s-pubkey.gpg", key)),
			[]byte(out), 0600)
		c.Assert(err, check.IsNil)
	}
}

func (s *CopySuite) TearDownSuite(c *check.C) {
	if s.registry != nil {
		s.registry.tearDown(c)
	}
	if s.s1Registry != nil {
		s.s1Registry.tearDown(c)
	}
	if s.cluster != nil {
		s.cluster.tearDown(c)
	}
}

func (s *CopySuite) TestCopyWithManifestList(c *check.C) {
	dir := c.MkDir()
	assertSkopeoSucceeds(c, "", "copy", knownListImage, "dir:"+dir)
}

func (s *CopySuite) TestCopyAllWithManifestList(c *check.C) {
	dir := c.MkDir()
	assertSkopeoSucceeds(c, "", "copy", "--all", knownListImage, "dir:"+dir)
}

func (s *CopySuite) TestCopyAllWithManifestListRoundTrip(c *check.C) {
	oci1 := c.MkDir()
	oci2 := c.MkDir()
	dir1 := c.MkDir()
	dir2 := c.MkDir()
	assertSkopeoSucceeds(c, "", "copy", "--multi-arch=all", knownListImage, "oci:"+oci1)
	assertSkopeoSucceeds(c, "", "copy", "--multi-arch=all", "oci:"+oci1, "dir:"+dir1)
	assertSkopeoSucceeds(c, "", "copy", "--multi-arch=all", "dir:"+dir1, "oci:"+oci2)
	assertSkopeoSucceeds(c, "", "copy", "--multi-arch=all", "oci:"+oci2, "dir:"+dir2)
	assertDirImagesAreEqual(c, dir1, dir2)
	out := combinedOutputOfCommand(c, "diff", "-urN", oci1, oci2)
	c.Assert(out, check.Equals, "")
}

func (s *CopySuite) TestCopyAllWithManifestListConverge(c *check.C) {
	oci1 := c.MkDir()
	oci2 := c.MkDir()
	dir1 := c.MkDir()
	dir2 := c.MkDir()
	assertSkopeoSucceeds(c, "", "copy", "--multi-arch=all", knownListImage, "oci:"+oci1)
	assertSkopeoSucceeds(c, "", "copy", "--multi-arch=all", "oci:"+oci1, "dir:"+dir1)
	assertSkopeoSucceeds(c, "", "copy", "--multi-arch=all", "--format", "oci", knownListImage, "dir:"+dir2)
	assertSkopeoSucceeds(c, "", "copy", "--multi-arch=all", "dir:"+dir2, "oci:"+oci2)
	assertDirImagesAreEqual(c, dir1, dir2)
	out := combinedOutputOfCommand(c, "diff", "-urN", oci1, oci2)
	c.Assert(out, check.Equals, "")
}

func (s *CopySuite) TestCopyNoneWithManifestList(c *check.C) {
	dir1 := c.MkDir()
	assertSkopeoSucceeds(c, "", "copy", "--multi-arch=index-only", knownListImage, "dir:"+dir1)

	manifestPath := filepath.Join(dir1, "manifest.json")
	readManifest, err := os.ReadFile(manifestPath)
	c.Assert(err, check.IsNil)
	mimeType := manifest.GuessMIMEType(readManifest)
	c.Assert(mimeType, check.Equals, "application/vnd.docker.distribution.manifest.list.v2+json")
	out := combinedOutputOfCommand(c, "ls", "-1", dir1)
	c.Assert(out, check.Equals, "manifest.json\nversion\n")
}

func (s *CopySuite) TestCopyWithManifestListConverge(c *check.C) {
	oci1 := c.MkDir()
	oci2 := c.MkDir()
	dir1 := c.MkDir()
	dir2 := c.MkDir()
	assertSkopeoSucceeds(c, "", "copy", knownListImage, "oci:"+oci1)
	assertSkopeoSucceeds(c, "", "copy", "--multi-arch=all", "oci:"+oci1, "dir:"+dir1)
	assertSkopeoSucceeds(c, "", "copy", "--format", "oci", knownListImage, "dir:"+dir2)
	assertSkopeoSucceeds(c, "", "copy", "--multi-arch=all", "dir:"+dir2, "oci:"+oci2)
	assertDirImagesAreEqual(c, dir1, dir2)
	out := combinedOutputOfCommand(c, "diff", "-urN", oci1, oci2)
	c.Assert(out, check.Equals, "")
}

func (s *CopySuite) TestCopyAllWithManifestListStorageFails(c *check.C) {
	storage := c.MkDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	assertSkopeoFails(c, `.*destination transport .* does not support copying multiple images as a group.*`, "copy", "--multi-arch=all", knownListImage, "containers-storage:"+storage+"test")
}

func (s *CopySuite) TestCopyWithManifestListStorage(c *check.C) {
	storage := c.MkDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	dir1 := c.MkDir()
	dir2 := c.MkDir()
	assertSkopeoSucceeds(c, "", "copy", knownListImage, "containers-storage:"+storage+"test")
	assertSkopeoSucceeds(c, "", "copy", knownListImage, "dir:"+dir1)
	assertSkopeoSucceeds(c, "", "copy", "containers-storage:"+storage+"test", "dir:"+dir2)
	runDecompressDirs(c, "", dir1, dir2)
	assertDirImagesAreEqual(c, dir1, dir2)
}

func (s *CopySuite) TestCopyWithManifestListStorageMultiple(c *check.C) {
	storage := c.MkDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	dir1 := c.MkDir()
	dir2 := c.MkDir()
	assertSkopeoSucceeds(c, "", "--override-arch", "amd64", "copy", knownListImage, "containers-storage:"+storage+"test")
	assertSkopeoSucceeds(c, "", "--override-arch", "arm64", "copy", knownListImage, "containers-storage:"+storage+"test")
	assertSkopeoSucceeds(c, "", "--override-arch", "arm64", "copy", knownListImage, "dir:"+dir1)
	assertSkopeoSucceeds(c, "", "copy", "containers-storage:"+storage+"test", "dir:"+dir2)
	runDecompressDirs(c, "", dir1, dir2)
	assertDirImagesAreEqual(c, dir1, dir2)
}

func (s *CopySuite) TestCopyWithManifestListDigest(c *check.C) {
	dir1 := c.MkDir()
	dir2 := c.MkDir()
	oci1 := c.MkDir()
	oci2 := c.MkDir()
	m := combinedOutputOfCommand(c, skopeoBinary, "inspect", "--raw", knownListImage)
	manifestDigest, err := manifest.Digest([]byte(m))
	c.Assert(err, check.IsNil)
	digest := manifestDigest.String()
	assertSkopeoSucceeds(c, "", "copy", knownListImage+"@"+digest, "dir:"+dir1)
	assertSkopeoSucceeds(c, "", "copy", "--multi-arch=all", knownListImage+"@"+digest, "dir:"+dir2)
	assertSkopeoSucceeds(c, "", "copy", "dir:"+dir1, "oci:"+oci1)
	assertSkopeoSucceeds(c, "", "copy", "dir:"+dir2, "oci:"+oci2)
	out := combinedOutputOfCommand(c, "diff", "-urN", oci1, oci2)
	c.Assert(out, check.Equals, "")
}

func (s *CopySuite) TestCopyWithDigestfileOutput(c *check.C) {
	tempdir := c.MkDir()
	dir1 := c.MkDir()
	digestOutPath := filepath.Join(tempdir, "digest.txt")
	assertSkopeoSucceeds(c, "", "copy", "--digestfile="+digestOutPath, knownListImage, "dir:"+dir1)
	readDigest, err := os.ReadFile(digestOutPath)
	c.Assert(err, check.IsNil)
	_, err = digest.Parse(string(readDigest))
	c.Assert(err, check.IsNil)
}

func (s *CopySuite) TestCopyWithManifestListStorageDigest(c *check.C) {
	storage := c.MkDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	dir1 := c.MkDir()
	dir2 := c.MkDir()
	m := combinedOutputOfCommand(c, skopeoBinary, "inspect", "--raw", knownListImage)
	manifestDigest, err := manifest.Digest([]byte(m))
	c.Assert(err, check.IsNil)
	digest := manifestDigest.String()
	assertSkopeoSucceeds(c, "", "copy", knownListImage+"@"+digest, "containers-storage:"+storage+"test@"+digest)
	assertSkopeoSucceeds(c, "", "copy", "containers-storage:"+storage+"test@"+digest, "dir:"+dir1)
	assertSkopeoSucceeds(c, "", "copy", knownListImage+"@"+digest, "dir:"+dir2)
	runDecompressDirs(c, "", dir1, dir2)
	assertDirImagesAreEqual(c, dir1, dir2)
}

func (s *CopySuite) TestCopyWithManifestListStorageDigestMultipleArches(c *check.C) {
	storage := c.MkDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	dir1 := c.MkDir()
	dir2 := c.MkDir()
	m := combinedOutputOfCommand(c, skopeoBinary, "inspect", "--raw", knownListImage)
	manifestDigest, err := manifest.Digest([]byte(m))
	c.Assert(err, check.IsNil)
	digest := manifestDigest.String()
	assertSkopeoSucceeds(c, "", "copy", knownListImage+"@"+digest, "containers-storage:"+storage+"test@"+digest)
	assertSkopeoSucceeds(c, "", "copy", "containers-storage:"+storage+"test@"+digest, "dir:"+dir1)
	assertSkopeoSucceeds(c, "", "copy", knownListImage+"@"+digest, "dir:"+dir2)
	runDecompressDirs(c, "", dir1, dir2)
	assertDirImagesAreEqual(c, dir1, dir2)
}

func (s *CopySuite) TestCopyWithManifestListStorageDigestMultipleArchesBothUseListDigest(c *check.C) {
	storage := c.MkDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	m := combinedOutputOfCommand(c, skopeoBinary, "inspect", "--raw", knownListImage)
	manifestDigest, err := manifest.Digest([]byte(m))
	c.Assert(err, check.IsNil)
	digest := manifestDigest.String()
	_, err = manifest.ListFromBlob([]byte(m), manifest.GuessMIMEType([]byte(m)))
	c.Assert(err, check.IsNil)
	assertSkopeoSucceeds(c, "", "--override-arch=amd64", "copy", knownListImage+"@"+digest, "containers-storage:"+storage+"test@"+digest)
	assertSkopeoSucceeds(c, "", "--override-arch=arm64", "copy", knownListImage+"@"+digest, "containers-storage:"+storage+"test@"+digest)
	assertSkopeoFails(c, `.*reading manifest for image instance.*does not exist.*`, "--override-arch=amd64", "inspect", "containers-storage:"+storage+"test@"+digest)
	assertSkopeoFails(c, `.*reading manifest for image instance.*does not exist.*`, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	i2 := combinedOutputOfCommand(c, skopeoBinary, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	var image2 imgspecv1.Image
	err = json.Unmarshal([]byte(i2), &image2)
	c.Assert(err, check.IsNil)
	c.Assert(image2.Architecture, check.Equals, "arm64")
}

func (s *CopySuite) TestCopyWithManifestListStorageDigestMultipleArchesFirstUsesListDigest(c *check.C) {
	storage := c.MkDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	m := combinedOutputOfCommand(c, skopeoBinary, "inspect", "--raw", knownListImage)
	manifestDigest, err := manifest.Digest([]byte(m))
	c.Assert(err, check.IsNil)
	digest := manifestDigest.String()
	list, err := manifest.ListFromBlob([]byte(m), manifest.GuessMIMEType([]byte(m)))
	c.Assert(err, check.IsNil)
	amd64Instance, err := list.ChooseInstance(&types.SystemContext{ArchitectureChoice: "amd64"})
	c.Assert(err, check.IsNil)
	arm64Instance, err := list.ChooseInstance(&types.SystemContext{ArchitectureChoice: "arm64"})
	c.Assert(err, check.IsNil)
	assertSkopeoSucceeds(c, "", "--override-arch=amd64", "copy", knownListImage+"@"+digest, "containers-storage:"+storage+"test@"+digest)
	assertSkopeoSucceeds(c, "", "--override-arch=arm64", "copy", knownListImage+"@"+arm64Instance.String(), "containers-storage:"+storage+"test@"+arm64Instance.String())
	i1 := combinedOutputOfCommand(c, skopeoBinary, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	var image1 imgspecv1.Image
	err = json.Unmarshal([]byte(i1), &image1)
	c.Assert(err, check.IsNil)
	c.Assert(image1.Architecture, check.Equals, "amd64")
	i2 := combinedOutputOfCommand(c, skopeoBinary, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test@"+amd64Instance.String())
	var image2 imgspecv1.Image
	err = json.Unmarshal([]byte(i2), &image2)
	c.Assert(err, check.IsNil)
	c.Assert(image2.Architecture, check.Equals, "amd64")
	assertSkopeoFails(c, `.*reading manifest for image instance.*does not exist.*`, "--override-arch=arm64", "inspect", "containers-storage:"+storage+"test@"+digest)
	assertSkopeoFails(c, `.*reading manifest for image instance.*does not exist.*`, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	i3 := combinedOutputOfCommand(c, skopeoBinary, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test@"+arm64Instance.String())
	var image3 imgspecv1.Image
	err = json.Unmarshal([]byte(i3), &image3)
	c.Assert(err, check.IsNil)
	c.Assert(image3.Architecture, check.Equals, "arm64")
}

func (s *CopySuite) TestCopyWithManifestListStorageDigestMultipleArchesSecondUsesListDigest(c *check.C) {
	storage := c.MkDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	m := combinedOutputOfCommand(c, skopeoBinary, "inspect", "--raw", knownListImage)
	manifestDigest, err := manifest.Digest([]byte(m))
	c.Assert(err, check.IsNil)
	digest := manifestDigest.String()
	list, err := manifest.ListFromBlob([]byte(m), manifest.GuessMIMEType([]byte(m)))
	c.Assert(err, check.IsNil)
	amd64Instance, err := list.ChooseInstance(&types.SystemContext{ArchitectureChoice: "amd64"})
	c.Assert(err, check.IsNil)
	arm64Instance, err := list.ChooseInstance(&types.SystemContext{ArchitectureChoice: "arm64"})
	c.Assert(err, check.IsNil)
	assertSkopeoSucceeds(c, "", "--override-arch=amd64", "copy", knownListImage+"@"+amd64Instance.String(), "containers-storage:"+storage+"test@"+amd64Instance.String())
	assertSkopeoSucceeds(c, "", "--override-arch=arm64", "copy", knownListImage+"@"+digest, "containers-storage:"+storage+"test@"+digest)
	i1 := combinedOutputOfCommand(c, skopeoBinary, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test@"+amd64Instance.String())
	var image1 imgspecv1.Image
	err = json.Unmarshal([]byte(i1), &image1)
	c.Assert(err, check.IsNil)
	c.Assert(image1.Architecture, check.Equals, "amd64")
	assertSkopeoFails(c, `.*reading manifest for image instance.*does not exist.*`, "--override-arch=amd64", "inspect", "containers-storage:"+storage+"test@"+digest)
	assertSkopeoFails(c, `.*reading manifest for image instance.*does not exist.*`, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	i2 := combinedOutputOfCommand(c, skopeoBinary, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	var image2 imgspecv1.Image
	err = json.Unmarshal([]byte(i2), &image2)
	c.Assert(err, check.IsNil)
	c.Assert(image2.Architecture, check.Equals, "arm64")
	i3 := combinedOutputOfCommand(c, skopeoBinary, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test@"+arm64Instance.String())
	var image3 imgspecv1.Image
	err = json.Unmarshal([]byte(i3), &image3)
	c.Assert(err, check.IsNil)
	c.Assert(image3.Architecture, check.Equals, "arm64")
}

func (s *CopySuite) TestCopyWithManifestListStorageDigestMultipleArchesThirdUsesListDigest(c *check.C) {
	storage := c.MkDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	m := combinedOutputOfCommand(c, skopeoBinary, "inspect", "--raw", knownListImage)
	manifestDigest, err := manifest.Digest([]byte(m))
	c.Assert(err, check.IsNil)
	digest := manifestDigest.String()
	list, err := manifest.ListFromBlob([]byte(m), manifest.GuessMIMEType([]byte(m)))
	c.Assert(err, check.IsNil)
	amd64Instance, err := list.ChooseInstance(&types.SystemContext{ArchitectureChoice: "amd64"})
	c.Assert(err, check.IsNil)
	arm64Instance, err := list.ChooseInstance(&types.SystemContext{ArchitectureChoice: "arm64"})
	c.Assert(err, check.IsNil)
	assertSkopeoSucceeds(c, "", "--override-arch=amd64", "copy", knownListImage+"@"+amd64Instance.String(), "containers-storage:"+storage+"test@"+amd64Instance.String())
	assertSkopeoSucceeds(c, "", "--override-arch=amd64", "copy", knownListImage+"@"+digest, "containers-storage:"+storage+"test@"+digest)
	assertSkopeoSucceeds(c, "", "--override-arch=arm64", "copy", knownListImage+"@"+digest, "containers-storage:"+storage+"test@"+digest)
	assertSkopeoFails(c, `.*reading manifest for image instance.*does not exist.*`, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	i1 := combinedOutputOfCommand(c, skopeoBinary, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test@"+amd64Instance.String())
	var image1 imgspecv1.Image
	err = json.Unmarshal([]byte(i1), &image1)
	c.Assert(err, check.IsNil)
	c.Assert(image1.Architecture, check.Equals, "amd64")
	i2 := combinedOutputOfCommand(c, skopeoBinary, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	var image2 imgspecv1.Image
	err = json.Unmarshal([]byte(i2), &image2)
	c.Assert(err, check.IsNil)
	c.Assert(image2.Architecture, check.Equals, "arm64")
	i3 := combinedOutputOfCommand(c, skopeoBinary, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test@"+arm64Instance.String())
	var image3 imgspecv1.Image
	err = json.Unmarshal([]byte(i3), &image3)
	c.Assert(err, check.IsNil)
	c.Assert(image3.Architecture, check.Equals, "arm64")
}

func (s *CopySuite) TestCopyWithManifestListStorageDigestMultipleArchesTagAndDigest(c *check.C) {
	storage := c.MkDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	m := combinedOutputOfCommand(c, skopeoBinary, "inspect", "--raw", knownListImage)
	manifestDigest, err := manifest.Digest([]byte(m))
	c.Assert(err, check.IsNil)
	digest := manifestDigest.String()
	list, err := manifest.ListFromBlob([]byte(m), manifest.GuessMIMEType([]byte(m)))
	c.Assert(err, check.IsNil)
	amd64Instance, err := list.ChooseInstance(&types.SystemContext{ArchitectureChoice: "amd64"})
	c.Assert(err, check.IsNil)
	arm64Instance, err := list.ChooseInstance(&types.SystemContext{ArchitectureChoice: "arm64"})
	c.Assert(err, check.IsNil)
	assertSkopeoSucceeds(c, "", "--override-arch=amd64", "copy", knownListImage, "containers-storage:"+storage+"test:latest")
	assertSkopeoSucceeds(c, "", "--override-arch=arm64", "copy", knownListImage+"@"+digest, "containers-storage:"+storage+"test@"+digest)
	assertSkopeoFails(c, `.*reading manifest for image instance.*does not exist.*`, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	i1 := combinedOutputOfCommand(c, skopeoBinary, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test:latest")
	var image1 imgspecv1.Image
	err = json.Unmarshal([]byte(i1), &image1)
	c.Assert(err, check.IsNil)
	c.Assert(image1.Architecture, check.Equals, "amd64")
	i2 := combinedOutputOfCommand(c, skopeoBinary, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test@"+amd64Instance.String())
	var image2 imgspecv1.Image
	err = json.Unmarshal([]byte(i2), &image2)
	c.Assert(err, check.IsNil)
	c.Assert(image2.Architecture, check.Equals, "amd64")
	i3 := combinedOutputOfCommand(c, skopeoBinary, "--override-arch=amd64", "inspect", "--config", "containers-storage:"+storage+"test:latest")
	var image3 imgspecv1.Image
	err = json.Unmarshal([]byte(i3), &image3)
	c.Assert(err, check.IsNil)
	c.Assert(image3.Architecture, check.Equals, "amd64")
	i4 := combinedOutputOfCommand(c, skopeoBinary, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test@"+arm64Instance.String())
	var image4 imgspecv1.Image
	err = json.Unmarshal([]byte(i4), &image4)
	c.Assert(err, check.IsNil)
	c.Assert(image4.Architecture, check.Equals, "arm64")
	i5 := combinedOutputOfCommand(c, skopeoBinary, "--override-arch=arm64", "inspect", "--config", "containers-storage:"+storage+"test@"+digest)
	var image5 imgspecv1.Image
	err = json.Unmarshal([]byte(i5), &image5)
	c.Assert(err, check.IsNil)
	c.Assert(image5.Architecture, check.Equals, "arm64")
}

func (s *CopySuite) TestCopyFailsWhenImageOSDoesNotMatchRuntimeOS(c *check.C) {
	storage := c.MkDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	assertSkopeoFails(c, `.*no image found in manifest list for architecture .*, variant .*, OS .*`, "copy", knownWindowsOnlyImage, "containers-storage:"+storage+"test")
}

func (s *CopySuite) TestCopySucceedsWhenImageDoesNotMatchRuntimeButWeOverride(c *check.C) {
	storage := c.MkDir()
	storage = fmt.Sprintf("[vfs@%s/root+%s/runroot]", storage, storage)
	assertSkopeoSucceeds(c, "", "--override-os=windows", "--override-arch=amd64", "copy", knownWindowsOnlyImage, "containers-storage:"+storage+"test")
}

func (s *CopySuite) TestCopySimpleAtomicRegistry(c *check.C) {
	dir1 := c.MkDir()
	dir2 := c.MkDir()

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	// "pull": docker: → dir:
	assertSkopeoSucceeds(c, "", "copy", testFQIN64, "dir:"+dir1)
	// "push": dir: → atomic:
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--debug", "copy", "dir:"+dir1, "atomic:localhost:5000/myns/unsigned:unsigned")
	// The result of pushing and pulling is an equivalent image, except for schema1 embedded names.
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "atomic:localhost:5000/myns/unsigned:unsigned", "dir:"+dir2)
	assertSchema1DirImagesAreEqualExceptNames(c, dir1, "libpod/busybox:amd64", dir2, "myns/unsigned:unsigned")
}

// The most basic (skopeo copy) use:
func (s *CopySuite) TestCopySimple(c *check.C) {
	const ourRegistry = "docker://" + v2DockerRegistryURL + "/"

	dir1 := c.MkDir()
	dir2 := c.MkDir()

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	// "pull": docker: → dir:
	assertSkopeoSucceeds(c, "", "copy", "docker://k8s.gcr.io/pause", "dir:"+dir1)
	// "push": dir: → docker(v2s2):
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--debug", "copy", "dir:"+dir1, ourRegistry+"pause:unsigned")
	// The result of pushing and pulling is an unmodified image.
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", ourRegistry+"pause:unsigned", "dir:"+dir2)
	out := combinedOutputOfCommand(c, "diff", "-urN", dir1, dir2)
	c.Assert(out, check.Equals, "")

	// docker v2s2 -> OCI image layout with image name
	// ociDest will be created by oci: if it doesn't exist
	// so don't create it here to exercise auto-creation
	ociDest := "pause-latest-image"
	ociImgName := "pause"
	defer os.RemoveAll(ociDest)
	assertSkopeoSucceeds(c, "", "copy", "docker://k8s.gcr.io/pause:latest", "oci:"+ociDest+":"+ociImgName)
	_, err := os.Stat(ociDest)
	c.Assert(err, check.IsNil)

	// docker v2s2 -> OCI image layout without image name
	ociDest = "pause-latest-noimage"
	defer os.RemoveAll(ociDest)
	assertSkopeoSucceeds(c, "", "copy", "docker://k8s.gcr.io/pause:latest", "oci:"+ociDest)
	_, err = os.Stat(ociDest)
	c.Assert(err, check.IsNil)
}

func (s *CopySuite) TestCopyEncryption(c *check.C) {
	originalImageDir := c.MkDir()
	encryptedImgDir := c.MkDir()
	decryptedImgDir := c.MkDir()
	keysDir := c.MkDir()
	undecryptedImgDir := c.MkDir()
	multiLayerImageDir := c.MkDir()
	partiallyEncryptedImgDir := c.MkDir()
	partiallyDecryptedImgDir := c.MkDir()

	// Create RSA key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	c.Assert(err, check.IsNil)
	publicKey := &privateKey.PublicKey
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	c.Assert(err, check.IsNil)
	err = os.WriteFile(keysDir+"/private.key", privateKeyBytes, 0644)
	c.Assert(err, check.IsNil)
	err = os.WriteFile(keysDir+"/public.key", publicKeyBytes, 0644)
	c.Assert(err, check.IsNil)

	// We can either perform encryption or decryption on the image.
	// This is why use should not be able to specify both encryption and decryption
	// during copy at the same time.
	assertSkopeoFails(c, ".*--encryption-key and --decryption-key cannot be specified together.*",
		"copy", "--encryption-key", "jwe:"+keysDir+"/public.key", "--decryption-key", keysDir+"/private.key",
		"oci:"+encryptedImgDir+":encrypted", "oci:"+decryptedImgDir+":decrypted")
	assertSkopeoFails(c, ".*--encryption-key and --decryption-key cannot be specified together.*",
		"copy", "--decryption-key", keysDir+"/private.key", "--encryption-key", "jwe:"+keysDir+"/public.key",
		"oci:"+encryptedImgDir+":encrypted", "oci:"+decryptedImgDir+":decrypted")

	// Copy a standard busybox image locally
	assertSkopeoSucceeds(c, "", "copy", testFQIN+":1.30.1", "oci:"+originalImageDir+":latest")

	// Encrypt the image
	assertSkopeoSucceeds(c, "", "copy", "--encryption-key",
		"jwe:"+keysDir+"/public.key", "oci:"+originalImageDir+":latest", "oci:"+encryptedImgDir+":encrypted")

	// An attempt to decrypt an encrypted image without a valid private key should fail
	invalidPrivateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	c.Assert(err, check.IsNil)
	invalidPrivateKeyBytes := x509.MarshalPKCS1PrivateKey(invalidPrivateKey)
	err = os.WriteFile(keysDir+"/invalid_private.key", invalidPrivateKeyBytes, 0644)
	c.Assert(err, check.IsNil)
	assertSkopeoFails(c, ".*no suitable key unwrapper found or none of the private keys could be used for decryption.*",
		"copy", "--decryption-key", keysDir+"/invalid_private.key",
		"oci:"+encryptedImgDir+":encrypted", "oci:"+decryptedImgDir+":decrypted")

	// Copy encrypted image without decrypting it
	assertSkopeoSucceeds(c, "", "copy", "oci:"+encryptedImgDir+":encrypted", "oci:"+undecryptedImgDir+":encrypted")
	// Original busybox image has gzipped layers. But encrypted busybox layers should
	// not be of gzip type
	matchLayerBlobBinaryType(c, undecryptedImgDir+"/blobs/sha256", "application/x-gzip", 0)

	// Decrypt the image
	assertSkopeoSucceeds(c, "", "copy", "--decryption-key", keysDir+"/private.key",
		"oci:"+undecryptedImgDir+":encrypted", "oci:"+decryptedImgDir+":decrypted")

	// After successful decryption we should find the gzipped layer from the
	// busybox image
	matchLayerBlobBinaryType(c, decryptedImgDir+"/blobs/sha256", "application/x-gzip", 1)

	// Copy a standard multi layer nginx image locally
	assertSkopeoSucceeds(c, "", "copy", testFQINMultiLayer, "oci:"+multiLayerImageDir+":latest")

	// Partially encrypt the image
	assertSkopeoSucceeds(c, "", "copy", "--encryption-key", "jwe:"+keysDir+"/public.key",
		"--encrypt-layer", "1", "oci:"+multiLayerImageDir+":latest", "oci:"+partiallyEncryptedImgDir+":encrypted")

	// Since the image is partially encrypted we should find layers that aren't encrypted
	matchLayerBlobBinaryType(c, partiallyEncryptedImgDir+"/blobs/sha256", "application/x-gzip", 2)

	// Decrypt the partially encrypted image
	assertSkopeoSucceeds(c, "", "copy", "--decryption-key", keysDir+"/private.key",
		"oci:"+partiallyEncryptedImgDir+":encrypted", "oci:"+partiallyDecryptedImgDir+":decrypted")

	// After successful decryption we should find the gzipped layers from the nginx image
	matchLayerBlobBinaryType(c, partiallyDecryptedImgDir+"/blobs/sha256", "application/x-gzip", 3)

}

func matchLayerBlobBinaryType(c *check.C, ociImageDirPath string, contentType string, matchCount int) {
	files, err := os.ReadDir(ociImageDirPath)
	c.Assert(err, check.IsNil)

	foundCount := 0
	for _, f := range files {
		fileContent, err := os.Open(ociImageDirPath + "/" + f.Name())
		c.Assert(err, check.IsNil)
		layerContentType, err := getFileContentType(fileContent)
		c.Assert(err, check.IsNil)

		if layerContentType == contentType {
			foundCount = foundCount + 1
		}
	}

	c.Assert(foundCount, check.Equals, matchCount)
}

func getFileContentType(out *os.File) (string, error) {
	buffer := make([]byte, 512)
	_, err := out.Read(buffer)
	if err != nil {
		return "", err
	}
	contentType := http.DetectContentType(buffer)

	return contentType, nil
}

// Check whether dir: images in dir1 and dir2 are equal, ignoring schema1 signatures.
func assertDirImagesAreEqual(c *check.C, dir1, dir2 string) {
	// The manifests may have different JWS signatures; so, compare the manifests by digests, which
	// strips the signatures.
	digests := []digest.Digest{}
	for _, dir := range []string{dir1, dir2} {
		manifestPath := filepath.Join(dir, "manifest.json")
		m, err := os.ReadFile(manifestPath)
		c.Assert(err, check.IsNil)
		digest, err := manifest.Digest(m)
		c.Assert(err, check.IsNil)
		digests = append(digests, digest)
	}
	c.Assert(digests[0], check.Equals, digests[1])
	// Then compare the rest file by file.
	out := combinedOutputOfCommand(c, "diff", "-urN", "-x", "manifest.json", dir1, dir2)
	c.Assert(out, check.Equals, "")
}

// Check whether schema1 dir: images in dir1 and dir2 are equal, ignoring schema1 signatures and the embedded path/tag values, which should have the expected values.
func assertSchema1DirImagesAreEqualExceptNames(c *check.C, dir1, ref1, dir2, ref2 string) {
	// The manifests may have different JWS signatures and names; so, unmarshal and delete these elements.
	manifests := []map[string]interface{}{}
	for dir, ref := range map[string]string{dir1: ref1, dir2: ref2} {
		manifestPath := filepath.Join(dir, "manifest.json")
		m, err := os.ReadFile(manifestPath)
		c.Assert(err, check.IsNil)
		data := map[string]interface{}{}
		err = json.Unmarshal(m, &data)
		c.Assert(err, check.IsNil)
		c.Assert(data["schemaVersion"], check.Equals, float64(1))
		colon := strings.LastIndex(ref, ":")
		c.Assert(colon, check.Not(check.Equals), -1)
		c.Assert(data["name"], check.Equals, ref[:colon])
		c.Assert(data["tag"], check.Equals, ref[colon+1:])
		for _, key := range []string{"signatures", "name", "tag"} {
			delete(data, key)
		}
		manifests = append(manifests, data)
	}
	c.Assert(manifests[0], check.DeepEquals, manifests[1])
	// Then compare the rest file by file.
	out := combinedOutputOfCommand(c, "diff", "-urN", "-x", "manifest.json", dir1, dir2)
	c.Assert(out, check.Equals, "")
}

// Streaming (skopeo copy)
func (s *CopySuite) TestCopyStreaming(c *check.C) {
	dir1 := c.MkDir()
	dir2 := c.MkDir()

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	// streaming: docker: → atomic:
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--debug", "copy", testFQIN64, "atomic:localhost:5000/myns/unsigned:streaming")
	// Compare (copies of) the original and the copy:
	assertSkopeoSucceeds(c, "", "copy", testFQIN64, "dir:"+dir1)
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "atomic:localhost:5000/myns/unsigned:streaming", "dir:"+dir2)
	assertSchema1DirImagesAreEqualExceptNames(c, dir1, "libpod/busybox:amd64", dir2, "myns/unsigned:streaming")
	// FIXME: Also check pushing to docker://
}

// OCI round-trip testing. It's very important to make sure that OCI <-> Docker
// conversion works (while skopeo handles many things, one of the most obvious
// benefits of a tool like skopeo is that you can use OCI tooling to create an
// image and then as the final step convert the image to a non-standard format
// like Docker). But this only works if we _test_ it.
func (s *CopySuite) TestCopyOCIRoundTrip(c *check.C) {
	const ourRegistry = "docker://" + v2DockerRegistryURL + "/"

	oci1 := c.MkDir()
	oci2 := c.MkDir()

	// Docker -> OCI
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--debug", "copy", testFQIN, "oci:"+oci1+":latest")
	// OCI -> Docker
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--debug", "copy", "oci:"+oci1+":latest", ourRegistry+"original/busybox:oci_copy")
	// Docker -> OCI
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--debug", "copy", ourRegistry+"original/busybox:oci_copy", "oci:"+oci2+":latest")
	// OCI -> Docker
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--debug", "copy", "oci:"+oci2+":latest", ourRegistry+"original/busybox:oci_copy2")

	// TODO: Add some more tags to output to and check those work properly.

	// First, make sure the OCI blobs are the same. This should _always_ be true.
	out := combinedOutputOfCommand(c, "diff", "-urN", oci1+"/blobs", oci2+"/blobs")
	c.Assert(out, check.Equals, "")

	// For some silly reason we pass a logger to the OCI library here...
	logger := log.New(os.Stderr, "", 0)

	// Verify using the upstream OCI image validator, this should catch most
	// non-compliance errors. DO NOT REMOVE THIS TEST UNLESS IT'S ABSOLUTELY
	// NECESSARY.
	err := image.ValidateLayout(oci1, nil, logger)
	c.Assert(err, check.IsNil)
	err = image.ValidateLayout(oci2, nil, logger)
	c.Assert(err, check.IsNil)

	// Now verify that everything is identical. Currently this is true, but
	// because we recompute the manifests on-the-fly this doesn't necessarily
	// always have to be true (but if this breaks in the future __PLEASE__ make
	// sure that the breakage actually makes sense before removing this check).
	out = combinedOutputOfCommand(c, "diff", "-urN", oci1, oci2)
	c.Assert(out, check.Equals, "")
}

// --sign-by and --policy copy, primarily using atomic:
func (s *CopySuite) TestCopySignatures(c *check.C) {
	mech, _, err := signature.NewEphemeralGPGSigningMechanism([]byte{})
	c.Assert(err, check.IsNil)
	defer mech.Close()
	if err := mech.SupportsSigning(); err != nil { // FIXME? Test that verification and policy enforcement works, using signatures from fixtures
		c.Skip(fmt.Sprintf("Signing not supported: %v", err))
	}

	dir := c.MkDir()
	dirDest := "dir:" + dir

	policy := fileFromFixture(c, "fixtures/policy.json", map[string]string{"@keydir@": s.gpgHome})
	defer os.Remove(policy)

	// type: reject
	assertSkopeoFails(c, fmt.Sprintf(".*Source image rejected: Running image %s:latest is rejected by policy.*", testFQIN),
		"--policy", policy, "copy", testFQIN+":latest", dirDest)

	// type: insecureAcceptAnything
	assertSkopeoSucceeds(c, "", "--policy", policy, "copy", "docker://quay.io/openshift/origin-hello-openshift", dirDest)

	// type: signedBy
	// Sign the images
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "--sign-by", "personal@example.com", testFQIN+":1.26", "atomic:localhost:5006/myns/personal:personal")
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "--sign-by", "official@example.com", testFQIN+":1.26.1", "atomic:localhost:5006/myns/official:official")
	// Verify that we can pull them
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--policy", policy, "copy", "atomic:localhost:5006/myns/personal:personal", dirDest)
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--policy", policy, "copy", "atomic:localhost:5006/myns/official:official", dirDest)
	// Verify that mis-signed images are rejected
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "atomic:localhost:5006/myns/personal:personal", "atomic:localhost:5006/myns/official:attack")
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "atomic:localhost:5006/myns/official:official", "atomic:localhost:5006/myns/personal:attack")
	assertSkopeoFails(c, ".*Source image rejected: Invalid GPG signature.*",
		"--tls-verify=false", "--policy", policy, "copy", "atomic:localhost:5006/myns/personal:attack", dirDest)
	assertSkopeoFails(c, ".*Source image rejected: Invalid GPG signature.*",
		"--tls-verify=false", "--policy", policy, "copy", "atomic:localhost:5006/myns/official:attack", dirDest)

	// Verify that signed identity is verified.
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "atomic:localhost:5006/myns/official:official", "atomic:localhost:5006/myns/naming:test1")
	assertSkopeoFails(c, ".*Source image rejected: Signature for identity localhost:5006/myns/official:official is not accepted.*",
		"--tls-verify=false", "--policy", policy, "copy", "atomic:localhost:5006/myns/naming:test1", dirDest)
	// signedIdentity works
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "atomic:localhost:5006/myns/official:official", "atomic:localhost:5006/myns/naming:naming")
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--policy", policy, "copy", "atomic:localhost:5006/myns/naming:naming", dirDest)

	// Verify that cosigning requirements are enforced
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "atomic:localhost:5006/myns/official:official", "atomic:localhost:5006/myns/cosigned:cosigned")
	assertSkopeoFails(c, ".*Source image rejected: Invalid GPG signature.*",
		"--tls-verify=false", "--policy", policy, "copy", "atomic:localhost:5006/myns/cosigned:cosigned", dirDest)

	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "--sign-by", "personal@example.com", "atomic:localhost:5006/myns/official:official", "atomic:localhost:5006/myns/cosigned:cosigned")
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--policy", policy, "copy", "atomic:localhost:5006/myns/cosigned:cosigned", dirDest)
}

// --policy copy for dir: sources
func (s *CopySuite) TestCopyDirSignatures(c *check.C) {
	mech, _, err := signature.NewEphemeralGPGSigningMechanism([]byte{})
	c.Assert(err, check.IsNil)
	defer mech.Close()
	if err := mech.SupportsSigning(); err != nil { // FIXME? Test that verification and policy enforcement works, using signatures from fixtures
		c.Skip(fmt.Sprintf("Signing not supported: %v", err))
	}

	topDir := c.MkDir()
	topDirDest := "dir:" + topDir

	for _, suffix := range []string{"/dir1", "/dir2", "/restricted/personal", "/restricted/official", "/restricted/badidentity", "/dest"} {
		err := os.MkdirAll(topDir+suffix, 0755)
		c.Assert(err, check.IsNil)
	}

	// Note the "/@dirpath@": The value starts with a slash so that it is not rejected in other tests which do not replace it,
	// but we must ensure that the result is a canonical path, not something starting with a "//".
	policy := fileFromFixture(c, "fixtures/policy.json", map[string]string{"@keydir@": s.gpgHome, "/@dirpath@": topDir + "/restricted"})
	defer os.Remove(policy)

	// Get some images.
	assertSkopeoSucceeds(c, "", "copy", testFQIN+":armfh", topDirDest+"/dir1")
	assertSkopeoSucceeds(c, "", "copy", testFQIN+":s390x", topDirDest+"/dir2")

	// Sign the images. By coping from a topDirDest/dirN, also test that non-/restricted paths
	// use the dir:"" default of insecureAcceptAnything.
	// (For signing, we must push to atomic: to get a Docker identity to use in the signature.)
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--policy", policy, "copy", "--sign-by", "personal@example.com", topDirDest+"/dir1", "atomic:localhost:5000/myns/personal:dirstaging")
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--policy", policy, "copy", "--sign-by", "official@example.com", topDirDest+"/dir2", "atomic:localhost:5000/myns/official:dirstaging")
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "atomic:localhost:5000/myns/personal:dirstaging", topDirDest+"/restricted/personal")
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "atomic:localhost:5000/myns/official:dirstaging", topDirDest+"/restricted/official")

	// type: signedBy, with a signedIdentity override (necessary because dir: identities can't be signed)
	// Verify that correct images are accepted
	assertSkopeoSucceeds(c, "", "--policy", policy, "copy", topDirDest+"/restricted/official", topDirDest+"/dest")
	// ... and that mis-signed images are rejected.
	assertSkopeoFails(c, ".*Source image rejected: Invalid GPG signature.*",
		"--policy", policy, "copy", topDirDest+"/restricted/personal", topDirDest+"/dest")

	// Verify that the signed identity is verified.
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--policy", policy, "copy", "--sign-by", "official@example.com", topDirDest+"/dir1", "atomic:localhost:5000/myns/personal:dirstaging2")
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "atomic:localhost:5000/myns/personal:dirstaging2", topDirDest+"/restricted/badidentity")
	assertSkopeoFails(c, ".*Source image rejected: .*Signature for identity localhost:5000/myns/personal:dirstaging2 is not accepted.*",
		"--policy", policy, "copy", topDirDest+"/restricted/badidentity", topDirDest+"/dest")
}

// Compression during copy
func (s *CopySuite) TestCopyCompression(c *check.C) {
	const uncompresssedLayerFile = "160d823fdc48e62f97ba62df31e55424f8f5eb6b679c865eec6e59adfe304710"

	topDir := c.MkDir()

	for i, t := range []struct{ fixture, remote string }{
		{"uncompressed-image-s1", "docker://" + v2DockerRegistryURL + "/compression/compression:s1"},
		{"uncompressed-image-s2", "docker://" + v2DockerRegistryURL + "/compression/compression:s2"},
		{"uncompressed-image-s1", "atomic:localhost:5000/myns/compression:s1"},
		{"uncompressed-image-s2", "atomic:localhost:5000/myns/compression:s2"},
	} {
		dir := filepath.Join(topDir, fmt.Sprintf("case%d", i))
		err := os.MkdirAll(dir, 0755)
		c.Assert(err, check.IsNil)

		assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "dir:fixtures/"+t.fixture, t.remote)
		assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", t.remote, "dir:"+dir)

		// The original directory contained an uncompressed file, the copy after pushing and pulling doesn't (we use a different name for the compressed file).
		_, err = os.Lstat(filepath.Join("fixtures", t.fixture, uncompresssedLayerFile))
		c.Assert(err, check.IsNil)
		_, err = os.Lstat(filepath.Join(dir, uncompresssedLayerFile))
		c.Assert(err, check.NotNil)
		c.Assert(os.IsNotExist(err), check.Equals, true)

		// All pulled layers are smaller than the uncompressed size of uncompresssedLayerFile. (Note that this includes the manifest in s2, but that works out OK).
		dirf, err := os.Open(dir)
		c.Assert(err, check.IsNil)
		fis, err := dirf.Readdir(-1)
		c.Assert(err, check.IsNil)
		for _, fi := range fis {
			c.Assert(fi.Size() < 2048, check.Equals, true)
		}
	}
}

func findRegularFiles(c *check.C, root string) []string {
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
	c.Assert(err, check.IsNil)
	return result
}

// --sign-by and policy use for docker: with lookaside
func (s *CopySuite) TestCopyDockerLookaside(c *check.C) {
	mech, _, err := signature.NewEphemeralGPGSigningMechanism([]byte{})
	c.Assert(err, check.IsNil)
	defer mech.Close()
	if err := mech.SupportsSigning(); err != nil { // FIXME? Test that verification and policy enforcement works, using signatures from fixtures
		c.Skip(fmt.Sprintf("Signing not supported: %v", err))
	}

	const ourRegistry = "docker://" + v2DockerRegistryURL + "/"

	tmpDir := c.MkDir()
	copyDest := filepath.Join(tmpDir, "dest")
	err = os.Mkdir(copyDest, 0755)
	c.Assert(err, check.IsNil)
	dirDest := "dir:" + copyDest
	plainLookaside := filepath.Join(tmpDir, "lookaside")
	splitLookasideStaging := filepath.Join(tmpDir, "lookaside-staging")

	splitLookasideReadServerHandler := http.NotFoundHandler()
	splitLookasideReadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		splitLookasideReadServerHandler.ServeHTTP(w, r)
	}))
	defer splitLookasideReadServer.Close()

	policy := fileFromFixture(c, "fixtures/policy.json", map[string]string{"@keydir@": s.gpgHome})
	defer os.Remove(policy)
	registriesDir := filepath.Join(tmpDir, "registries.d")
	err = os.Mkdir(registriesDir, 0755)
	c.Assert(err, check.IsNil)
	registriesFile := fileFromFixture(c, "fixtures/registries.yaml",
		map[string]string{"@lookaside@": plainLookaside, "@split-staging@": splitLookasideStaging, "@split-read@": splitLookasideReadServer.URL})
	err = os.Symlink(registriesFile, filepath.Join(registriesDir, "registries.yaml"))
	c.Assert(err, check.IsNil)

	// Get an image to work with.  Also verifies that we can use Docker repositories with no lookaside configured.
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--registries.d", registriesDir, "copy", testFQIN, ourRegistry+"original/busybox")
	// Pulling an unsigned image fails.
	assertSkopeoFails(c, ".*Source image rejected: A signature was required, but no signature exists.*",
		"--tls-verify=false", "--policy", policy, "--registries.d", registriesDir, "copy", ourRegistry+"original/busybox", dirDest)

	// Signing with lookaside defined succeeds,
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--registries.d", registriesDir, "copy", "--sign-by", "personal@example.com", ourRegistry+"original/busybox", ourRegistry+"signed/busybox")
	// a signature file has been created,
	foundFiles := findRegularFiles(c, plainLookaside)
	c.Assert(foundFiles, check.HasLen, 1)
	// and pulling a signed image succeeds.
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--policy", policy, "--registries.d", registriesDir, "copy", ourRegistry+"signed/busybox", dirDest)

	// Deleting the image succeeds,
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--registries.d", registriesDir, "delete", ourRegistry+"signed/busybox")
	// and the signature file has been deleted (but we leave the directories around).
	foundFiles = findRegularFiles(c, plainLookaside)
	c.Assert(foundFiles, check.HasLen, 0)

	// Signing with a read/write lookaside split succeeds,
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--registries.d", registriesDir, "copy", "--sign-by", "personal@example.com", ourRegistry+"original/busybox", ourRegistry+"public/busybox")
	// and a signature file has been created.
	foundFiles = findRegularFiles(c, splitLookasideStaging)
	c.Assert(foundFiles, check.HasLen, 1)
	// Pulling the image fails because the read lookaside URL has not been populated:
	assertSkopeoFails(c, ".*Source image rejected: A signature was required, but no signature exists.*",
		"--tls-verify=false", "--policy", policy, "--registries.d", registriesDir, "copy", ourRegistry+"public/busybox", dirDest)
	// Pulling the image succeeds after the read lookaside URL is available:
	splitLookasideReadServerHandler = http.FileServer(http.Dir(splitLookasideStaging))
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--policy", policy, "--registries.d", registriesDir, "copy", ourRegistry+"public/busybox", dirDest)
}

// atomic: and docker: X-Registry-Supports-Signatures works and interoperates
func (s *CopySuite) TestCopyAtomicExtension(c *check.C) {
	mech, _, err := signature.NewEphemeralGPGSigningMechanism([]byte{})
	c.Assert(err, check.IsNil)
	defer mech.Close()
	if err := mech.SupportsSigning(); err != nil { // FIXME? Test that the reading/writing works using signatures from fixtures
		c.Skip(fmt.Sprintf("Signing not supported: %v", err))
	}

	topDir := c.MkDir()
	for _, subdir := range []string{"dirAA", "dirAD", "dirDA", "dirDD", "registries.d"} {
		err := os.MkdirAll(filepath.Join(topDir, subdir), 0755)
		c.Assert(err, check.IsNil)
	}
	registriesDir := filepath.Join(topDir, "registries.d")
	dirDest := "dir:" + topDir
	policy := fileFromFixture(c, "fixtures/policy.json", map[string]string{"@keydir@": s.gpgHome})
	defer os.Remove(policy)

	// Get an image to work with to an atomic: destination.  Also verifies that we can use Docker repositories without X-Registry-Supports-Signatures
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--registries.d", registriesDir, "copy", testFQIN, "atomic:localhost:5000/myns/extension:unsigned")
	// Pulling an unsigned image using atomic: fails.
	assertSkopeoFails(c, ".*Source image rejected: A signature was required, but no signature exists.*",
		"--tls-verify=false", "--policy", policy,
		"copy", "atomic:localhost:5000/myns/extension:unsigned", dirDest+"/dirAA")
	// The same when pulling using docker:
	assertSkopeoFails(c, ".*Source image rejected: A signature was required, but no signature exists.*",
		"--tls-verify=false", "--policy", policy, "--registries.d", registriesDir,
		"copy", "docker://localhost:5000/myns/extension:unsigned", dirDest+"/dirAD")

	// Sign the image using atomic:
	assertSkopeoSucceeds(c, "", "--tls-verify=false",
		"copy", "--sign-by", "personal@example.com", "atomic:localhost:5000/myns/extension:unsigned", "atomic:localhost:5000/myns/extension:atomic")
	// Pulling the image using atomic: now succeeds.
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--policy", policy,
		"copy", "atomic:localhost:5000/myns/extension:atomic", dirDest+"/dirAA")
	// The same when pulling using docker:
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--policy", policy, "--registries.d", registriesDir,
		"copy", "docker://localhost:5000/myns/extension:atomic", dirDest+"/dirAD")
	// Both access methods result in the same data.
	assertDirImagesAreEqual(c, filepath.Join(topDir, "dirAA"), filepath.Join(topDir, "dirAD"))

	// Get another image (different so that they don't share signatures, and sign it using docker://)
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "--registries.d", registriesDir,
		"copy", "--sign-by", "personal@example.com", testFQIN+":ppc64le", "docker://localhost:5000/myns/extension:extension")
	c.Logf("%s", combinedOutputOfCommand(c, "oc", "get", "istag", "extension:extension", "-o", "json"))
	// Pulling the image using atomic: succeeds.
	assertSkopeoSucceeds(c, "", "--debug", "--tls-verify=false", "--policy", policy,
		"copy", "atomic:localhost:5000/myns/extension:extension", dirDest+"/dirDA")
	// The same when pulling using docker:
	assertSkopeoSucceeds(c, "", "--debug", "--tls-verify=false", "--policy", policy, "--registries.d", registriesDir,
		"copy", "docker://localhost:5000/myns/extension:extension", dirDest+"/dirDD")
	// Both access methods result in the same data.
	assertDirImagesAreEqual(c, filepath.Join(topDir, "dirDA"), filepath.Join(topDir, "dirDD"))
}

// Both mirroring support in registries.conf, and mirrored remapIdentity support in policy.json
func (s *CopySuite) TestCopyVerifyingMirroredSignatures(c *check.C) {
	const regPrefix = "docker://localhost:5006/myns/mirroring-"

	mech, _, err := signature.NewEphemeralGPGSigningMechanism([]byte{})
	c.Assert(err, check.IsNil)
	defer mech.Close()
	if err := mech.SupportsSigning(); err != nil { // FIXME? Test that verification and policy enforcement works, using signatures from fixtures
		c.Skip(fmt.Sprintf("Signing not supported: %v", err))
	}

	topDir := c.MkDir()
	registriesDir := filepath.Join(topDir, "registries.d") // An empty directory to disable lookaside use
	dirDest := "dir:" + filepath.Join(topDir, "unused-dest")

	policy := fileFromFixture(c, "fixtures/policy.json", map[string]string{"@keydir@": s.gpgHome})
	defer os.Remove(policy)

	// We use X-R-S-S for this testing to avoid having to deal with the lookasides.
	// A downside is that OpenShift records signatures per image, so the error messages below
	// list all signatures for other tags used for the same image as well.
	// So, make sure to never create a signature that could be considered valid in a different part of the test (i.e. don't reuse tags).

	// Get an image to work with.
	assertSkopeoSucceeds(c, "", "copy", "--dest-tls-verify=false", testFQIN, regPrefix+"primary:unsigned")
	// Verify that unsigned images are rejected
	assertSkopeoFails(c, ".*Source image rejected: A signature was required, but no signature exists.*",
		"--policy", policy, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"primary:unsigned", dirDest)
	// Sign the image for the primary location
	assertSkopeoSucceeds(c, "", "--registries.d", registriesDir, "copy", "--src-tls-verify=false", "--dest-tls-verify=false", "--sign-by", "personal@example.com", regPrefix+"primary:unsigned", regPrefix+"primary:direct")
	// Verify that a correctly signed image in the primary location is usable.
	assertSkopeoSucceeds(c, "", "--policy", policy, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"primary:direct", dirDest)

	// Sign the image for the mirror
	assertSkopeoSucceeds(c, "", "--registries.d", registriesDir, "copy", "--src-tls-verify=false", "--dest-tls-verify=false", "--sign-by", "personal@example.com", regPrefix+"primary:unsigned", regPrefix+"mirror:mirror-signed")
	// Verify that a correctly signed image for the mirror is accessible using the mirror's reference
	assertSkopeoSucceeds(c, "", "--policy", policy, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"mirror:mirror-signed", dirDest)
	// … but verify that while it is accessible using the primary location redirecting to the mirror, …
	assertSkopeoSucceeds(c, "" /* no --policy */, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"primary:mirror-signed", dirDest)
	// … verify it is NOT accessible when requiring a signature.
	assertSkopeoFails(c, ".*Source image rejected: None of the signatures were accepted, reasons: Signature for identity localhost:5006/myns/mirroring-primary:direct is not accepted; Signature for identity localhost:5006/myns/mirroring-mirror:mirror-signed is not accepted.*",
		"--policy", policy, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"primary:mirror-signed", dirDest)

	// Fail if we specify an unqualified identity
	assertSkopeoFails(c, ".*Could not parse --sign-identity: repository name must be canonical.*",
		"--registries.d", registriesDir, "copy", "--src-tls-verify=false", "--dest-tls-verify=false", "--sign-by=personal@example.com", "--sign-identity=this-is-not-fully-specified", regPrefix+"primary:unsigned", regPrefix+"mirror:primary-signed")

	// Create a signature for mirroring-primary:primary-signed without pushing there.
	assertSkopeoSucceeds(c, "", "--registries.d", registriesDir, "copy", "--src-tls-verify=false", "--dest-tls-verify=false", "--sign-by=personal@example.com", "--sign-identity=localhost:5006/myns/mirroring-primary:primary-signed", regPrefix+"primary:unsigned", regPrefix+"mirror:primary-signed")
	// Verify that a correctly signed image for the primary is accessible using the primary's reference
	assertSkopeoSucceeds(c, "", "--policy", policy, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"primary:primary-signed", dirDest)
	// … but verify that while it is accessible using the mirror location
	assertSkopeoSucceeds(c, "" /* no --policy */, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"mirror:primary-signed", dirDest)
	// … verify it is NOT accessible when requiring a signature.
	assertSkopeoFails(c, ".*Source image rejected: None of the signatures were accepted, reasons: Signature for identity localhost:5006/myns/mirroring-primary:direct is not accepted; Signature for identity localhost:5006/myns/mirroring-mirror:mirror-signed is not accepted; Signature for identity localhost:5006/myns/mirroring-primary:primary-signed is not accepted.*",
		"--policy", policy, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"mirror:primary-signed", dirDest)

	assertSkopeoSucceeds(c, "", "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", "--dest-tls-verify=false", regPrefix+"primary:unsigned", regPrefix+"remap:remapped")
	// Verify that while a remapIdentity image is accessible using the remapped (mirror) location
	assertSkopeoSucceeds(c, "" /* no --policy */, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"remap:remapped", dirDest)
	// … it is NOT accessible when requiring a signature …
	assertSkopeoFails(c, ".*Source image rejected: None of the signatures were accepted, reasons: Signature for identity localhost:5006/myns/mirroring-primary:direct is not accepted; Signature for identity localhost:5006/myns/mirroring-mirror:mirror-signed is not accepted; Signature for identity localhost:5006/myns/mirroring-primary:primary-signed is not accepted.*", "--policy", policy, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"remap:remapped", dirDest)
	// … until signed.
	assertSkopeoSucceeds(c, "", "--registries.d", registriesDir, "copy", "--src-tls-verify=false", "--dest-tls-verify=false", "--sign-by=personal@example.com", "--sign-identity=localhost:5006/myns/mirroring-primary:remapped", regPrefix+"remap:remapped", regPrefix+"remap:remapped")
	assertSkopeoSucceeds(c, "", "--policy", policy, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"remap:remapped", dirDest)
	// To be extra clear about the semantics, verify that the signedPrefix (primary) location never exists
	// and only the remapped prefix (mirror) is accessed.
	assertSkopeoFails(c, ".*initializing source docker://localhost:5006/myns/mirroring-primary:remapped:.*manifest unknown: manifest unknown.*", "--policy", policy, "--registries.d", registriesDir, "--registries-conf", "fixtures/registries.conf", "copy", "--src-tls-verify=false", regPrefix+"primary:remapped", dirDest)
}

func (s *SkopeoSuite) TestCopySrcWithAuth(c *check.C) {
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "--dest-creds=testuser:testpassword", testFQIN, fmt.Sprintf("docker://%s/busybox:latest", s.regV2WithAuth.url))
	dir1 := c.MkDir()
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "--src-creds=testuser:testpassword", fmt.Sprintf("docker://%s/busybox:latest", s.regV2WithAuth.url), "dir:"+dir1)
}

func (s *SkopeoSuite) TestCopyDestWithAuth(c *check.C) {
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "--dest-creds=testuser:testpassword", testFQIN, fmt.Sprintf("docker://%s/busybox:latest", s.regV2WithAuth.url))
}

func (s *SkopeoSuite) TestCopySrcAndDestWithAuth(c *check.C) {
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "--dest-creds=testuser:testpassword", testFQIN, fmt.Sprintf("docker://%s/busybox:latest", s.regV2WithAuth.url))
	assertSkopeoSucceeds(c, "", "--tls-verify=false", "copy", "--src-creds=testuser:testpassword", "--dest-creds=testuser:testpassword", fmt.Sprintf("docker://%s/busybox:latest", s.regV2WithAuth.url), fmt.Sprintf("docker://%s/test:auth", s.regV2WithAuth.url))
}

func (s *CopySuite) TestCopyNoPanicOnHTTPResponseWithoutTLSVerifyFalse(c *check.C) {
	topDir := c.MkDir()

	const ourRegistry = "docker://" + v2DockerRegistryURL + "/"

	assertSkopeoFails(c, ".*server gave HTTP response to HTTPS client.*",
		"copy", ourRegistry+"foobar", "dir:"+topDir)
}

func (s *CopySuite) TestCopySchemaConversion(c *check.C) {
	// Test conversion / schema autodetection both for the OpenShift embedded registry…
	s.testCopySchemaConversionRegistries(c, "docker://localhost:5005/myns/schema1", "docker://localhost:5006/myns/schema2")
	// … and for various docker/distribution registry versions.
	s.testCopySchemaConversionRegistries(c, "docker://"+v2s1DockerRegistryURL+"/schema1", "docker://"+v2DockerRegistryURL+"/schema2")
}

func (s *CopySuite) TestCopyManifestConversion(c *check.C) {
	topDir := c.MkDir()
	srcDir := filepath.Join(topDir, "source")
	destDir1 := filepath.Join(topDir, "dest1")
	destDir2 := filepath.Join(topDir, "dest2")

	// oci to v2s1 and vice-versa not supported yet
	// get v2s2 manifest type
	assertSkopeoSucceeds(c, "", "copy", testFQIN, "dir:"+srcDir)
	verifyManifestMIMEType(c, srcDir, manifest.DockerV2Schema2MediaType)
	// convert from v2s2 to oci
	assertSkopeoSucceeds(c, "", "copy", "--format=oci", "dir:"+srcDir, "dir:"+destDir1)
	verifyManifestMIMEType(c, destDir1, imgspecv1.MediaTypeImageManifest)
	// convert from oci to v2s2
	assertSkopeoSucceeds(c, "", "copy", "--format=v2s2", "dir:"+destDir1, "dir:"+destDir2)
	verifyManifestMIMEType(c, destDir2, manifest.DockerV2Schema2MediaType)
	// convert from v2s2 to v2s1
	assertSkopeoSucceeds(c, "", "copy", "--format=v2s1", "dir:"+srcDir, "dir:"+destDir1)
	verifyManifestMIMEType(c, destDir1, manifest.DockerV2Schema1SignedMediaType)
	// convert from v2s1 to v2s2
	assertSkopeoSucceeds(c, "", "copy", "--format=v2s2", "dir:"+destDir1, "dir:"+destDir2)
	verifyManifestMIMEType(c, destDir2, manifest.DockerV2Schema2MediaType)
}

func (s *CopySuite) TestCopyPreserveDigests(c *check.C) {
	topDir := c.MkDir()

	assertSkopeoSucceeds(c, "", "copy", knownListImage, "--multi-arch=all", "--preserve-digests", "dir:"+topDir)
	assertSkopeoFails(c, ".*Instructed to preserve digests.*", "copy", knownListImage, "--multi-arch=all", "--preserve-digests", "--format=oci", "dir:"+topDir)
}

func (s *CopySuite) testCopySchemaConversionRegistries(c *check.C, schema1Registry, schema2Registry string) {
	topDir := c.MkDir()
	for _, subdir := range []string{"input1", "input2", "dest2"} {
		err := os.MkdirAll(filepath.Join(topDir, subdir), 0755)
		c.Assert(err, check.IsNil)
	}
	input1Dir := filepath.Join(topDir, "input1")
	input2Dir := filepath.Join(topDir, "input2")
	destDir := filepath.Join(topDir, "dest2")

	// Ensure we are working with a schema2 image.
	// dir: accepts any manifest format, i.e. this makes …/input2 a schema2 source which cannot be asked to produce schema1 like ordinary docker: registries can.
	assertSkopeoSucceeds(c, "", "copy", testFQIN, "dir:"+input2Dir)
	verifyManifestMIMEType(c, input2Dir, manifest.DockerV2Schema2MediaType)
	// 2→2 (the "f2t2" in tag means "from 2 to 2")
	assertSkopeoSucceeds(c, "", "copy", "--dest-tls-verify=false", "dir:"+input2Dir, schema2Registry+":f2t2")
	assertSkopeoSucceeds(c, "", "copy", "--src-tls-verify=false", schema2Registry+":f2t2", "dir:"+destDir)
	verifyManifestMIMEType(c, destDir, manifest.DockerV2Schema2MediaType)
	// 2→1; we will use the result as a schema1 image for further tests.
	assertSkopeoSucceeds(c, "", "copy", "--dest-tls-verify=false", "dir:"+input2Dir, schema1Registry+":f2t1")
	assertSkopeoSucceeds(c, "", "copy", "--src-tls-verify=false", schema1Registry+":f2t1", "dir:"+input1Dir)
	verifyManifestMIMEType(c, input1Dir, manifest.DockerV2Schema1SignedMediaType)
	// 1→1
	assertSkopeoSucceeds(c, "", "copy", "--dest-tls-verify=false", "dir:"+input1Dir, schema1Registry+":f1t1")
	assertSkopeoSucceeds(c, "", "copy", "--src-tls-verify=false", schema1Registry+":f1t1", "dir:"+destDir)
	verifyManifestMIMEType(c, destDir, manifest.DockerV2Schema1SignedMediaType)
	// 1→2: image stays unmodified schema1
	assertSkopeoSucceeds(c, "", "copy", "--dest-tls-verify=false", "dir:"+input1Dir, schema2Registry+":f1t2")
	assertSkopeoSucceeds(c, "", "copy", "--src-tls-verify=false", schema2Registry+":f1t2", "dir:"+destDir)
	verifyManifestMIMEType(c, destDir, manifest.DockerV2Schema1SignedMediaType)
}

const regConfFixture = "./fixtures/registries.conf"

func (s *SkopeoSuite) TestSuccessCopySrcWithMirror(c *check.C) {
	dir := c.MkDir()

	assertSkopeoSucceeds(c, "", "--registries-conf="+regConfFixture, "copy",
		"docker://mirror.invalid/busybox", "dir:"+dir)
}

func (s *SkopeoSuite) TestFailureCopySrcWithMirrorsUnavailable(c *check.C) {
	dir := c.MkDir()

	// .invalid domains are, per RFC 6761, supposed to result in NXDOMAIN.
	// With systemd-resolved (used only via NSS?), we instead seem to get “Temporary failure in name resolution”
	assertSkopeoFails(c, ".*(no such host|Temporary failure in name resolution).*",
		"--registries-conf="+regConfFixture, "copy", "docker://invalid.invalid/busybox", "dir:"+dir)
}

func (s *SkopeoSuite) TestSuccessCopySrcWithMirrorAndPrefix(c *check.C) {
	dir := c.MkDir()

	assertSkopeoSucceeds(c, "", "--registries-conf="+regConfFixture, "copy",
		"docker://gcr.invalid/foo/bar/busybox", "dir:"+dir)
}

func (s *SkopeoSuite) TestFailureCopySrcWithMirrorAndPrefixUnavailable(c *check.C) {
	dir := c.MkDir()

	// .invalid domains are, per RFC 6761, supposed to result in NXDOMAIN.
	// With systemd-resolved (used only via NSS?), we instead seem to get “Temporary failure in name resolution”
	assertSkopeoFails(c, ".*(no such host|Temporary failure in name resolution).*",
		"--registries-conf="+regConfFixture, "copy", "docker://gcr.invalid/wrong/prefix/busybox", "dir:"+dir)
}

func (s *CopySuite) TestCopyFailsWhenReferenceIsInvalid(c *check.C) {
	assertSkopeoFails(c, `.*Invalid image name.*`, "copy", "unknown:transport", "unknown:test")
}
