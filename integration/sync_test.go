package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/containers/image/v5/docker"
	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/types"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

const (
	// A repository with a path with multiple components in it which
	// contains multiple tags, preferably with some tags pointing to
	// manifest lists, and with some tags that don't.
	pullableRepo = "registry.k8s.io/coredns/coredns"
	// A tagged image in the repository that we can inspect and copy.
	pullableTaggedImage = "registry.k8s.io/coredns/coredns:v1.6.6"
	// A tagged manifest list in the repository that we can inspect and copy.
	pullableTaggedManifestList = "registry.k8s.io/coredns/coredns:v1.8.0"
	// A repository containing multiple tags, some of which are for
	// manifest lists, and which includes a "latest" tag.  We specify the
	// name here without a tag.
	pullableRepoWithLatestTag = "registry.k8s.io/pause"
)

func TestSync(t *testing.T) {
	suite.Run(t, &syncSuite{})
}

type syncSuite struct {
	suite.Suite
	cluster  *openshiftCluster
	registry *testRegistryV2
}

var _ = suite.SetupAllSuite(&syncSuite{})
var _ = suite.TearDownAllSuite(&syncSuite{})

func (s *syncSuite) SetupSuite() {
	t := s.T()

	const registryAuth = false
	const registrySchema1 = false

	if os.Getenv("SKOPEO_LOCAL_TESTS") == "1" {
		t.Log("Running tests without a container")
		fmt.Printf("NOTE: tests requires a V2 registry at url=%s, with auth=%t, schema1=%t \n", v2DockerRegistryURL, registryAuth, registrySchema1)
		return
	}

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
	s.registry = setupRegistryV2At(t, v2DockerRegistryURL, registryAuth, registrySchema1)

	gpgHome := t.TempDir()
	t.Setenv("GNUPGHOME", gpgHome)

	for _, key := range []string{"personal", "official"} {
		batchInput := fmt.Sprintf("Key-Type: RSA\nName-Real: Test key - %s\nName-email: %s@example.com\n%%no-protection\n%%commit\n",
			key, key)
		runCommandWithInput(t, batchInput, gpgBinary, "--batch", "--gen-key")

		out := combinedOutputOfCommand(t, gpgBinary, "--armor", "--export", fmt.Sprintf("%s@example.com", key))
		err := os.WriteFile(filepath.Join(gpgHome, fmt.Sprintf("%s-pubkey.gpg", key)),
			[]byte(out), 0600)
		require.NoError(t, err)
	}
}

func (s *syncSuite) TearDownSuite() {
	t := s.T()
	if os.Getenv("SKOPEO_LOCAL_TESTS") == "1" {
		return
	}

	if s.registry != nil {
		s.registry.tearDown()
	}
	if s.cluster != nil {
		s.cluster.tearDown(t)
	}
}

func assertNumberOfManifestsInSubdirs(t *testing.T, dir string, expectedCount int) {
	nManifests := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "manifest.json" {
			nManifests++
			return filepath.SkipDir
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, expectedCount, nManifests)
}

func (s *syncSuite) TestDocker2DirTagged() {
	t := s.T()
	tmpDir := t.TempDir()

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	image := pullableTaggedImage
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	require.NoError(t, err)
	imagePath := imageRef.DockerReference().String()

	dir1 := path.Join(tmpDir, "dir1")
	dir2 := path.Join(tmpDir, "dir2")

	// sync docker => dir
	assertSkopeoSucceeds(t, "", "sync", "--scoped", "--src", "docker", "--dest", "dir", image, dir1)
	_, err = os.Stat(path.Join(dir1, imagePath, "manifest.json"))
	require.NoError(t, err)

	// copy docker => dir
	assertSkopeoSucceeds(t, "", "copy", "docker://"+image, "dir:"+dir2)
	_, err = os.Stat(path.Join(dir2, "manifest.json"))
	require.NoError(t, err)

	out := combinedOutputOfCommand(t, "diff", "-urN", path.Join(dir1, imagePath), dir2)
	assert.Equal(t, "", out)
}

func (s *syncSuite) TestDocker2DirTaggedAll() {
	t := s.T()
	tmpDir := t.TempDir()

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	image := pullableTaggedManifestList
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	require.NoError(t, err)
	imagePath := imageRef.DockerReference().String()

	dir1 := path.Join(tmpDir, "dir1")
	dir2 := path.Join(tmpDir, "dir2")

	// sync docker => dir
	assertSkopeoSucceeds(t, "", "sync", "--all", "--scoped", "--src", "docker", "--dest", "dir", image, dir1)
	_, err = os.Stat(path.Join(dir1, imagePath, "manifest.json"))
	require.NoError(t, err)

	// copy docker => dir
	assertSkopeoSucceeds(t, "", "copy", "--all", "docker://"+image, "dir:"+dir2)
	_, err = os.Stat(path.Join(dir2, "manifest.json"))
	require.NoError(t, err)

	out := combinedOutputOfCommand(t, "diff", "-urN", path.Join(dir1, imagePath), dir2)
	assert.Equal(t, "", out)
}

func (s *syncSuite) TestPreserveDigests() {
	t := s.T()
	tmpDir := t.TempDir()

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	image := pullableTaggedManifestList

	// copy docker => dir
	assertSkopeoSucceeds(t, "", "copy", "--all", "--preserve-digests", "docker://"+image, "dir:"+tmpDir)
	_, err := os.Stat(path.Join(tmpDir, "manifest.json"))
	require.NoError(t, err)

	assertSkopeoFails(t, ".*Instructed to preserve digests.*", "copy", "--all", "--preserve-digests", "--format=oci", "docker://"+image, "dir:"+tmpDir)
}

func (s *syncSuite) TestScoped() {
	t := s.T()
	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	image := pullableTaggedImage
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	require.NoError(t, err)
	imagePath := imageRef.DockerReference().String()

	dir1 := t.TempDir()
	assertSkopeoSucceeds(t, "", "sync", "--src", "docker", "--dest", "dir", image, dir1)
	_, err = os.Stat(path.Join(dir1, path.Base(imagePath), "manifest.json"))
	require.NoError(t, err)

	assertSkopeoSucceeds(t, "", "sync", "--scoped", "--src", "docker", "--dest", "dir", image, dir1)
	_, err = os.Stat(path.Join(dir1, imagePath, "manifest.json"))
	require.NoError(t, err)
}

func (s *syncSuite) TestDirIsNotOverwritten() {
	t := s.T()
	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	image := pullableRepoWithLatestTag
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	require.NoError(t, err)
	imagePath := imageRef.DockerReference().String()

	// make a copy of the image in the local registry
	assertSkopeoSucceeds(t, "", "copy", "--dest-tls-verify=false", "docker://"+image, "docker://"+path.Join(v2DockerRegistryURL, reference.Path(imageRef.DockerReference())))

	//sync upstream image to dir, not scoped
	dir1 := t.TempDir()
	assertSkopeoSucceeds(t, "", "sync", "--src", "docker", "--dest", "dir", image, dir1)
	_, err = os.Stat(path.Join(dir1, path.Base(imagePath), "manifest.json"))
	require.NoError(t, err)

	//sync local registry image to dir, not scoped
	assertSkopeoFails(t, ".*Refusing to overwrite destination directory.*", "sync", "--src-tls-verify=false", "--src", "docker", "--dest", "dir", path.Join(v2DockerRegistryURL, reference.Path(imageRef.DockerReference())), dir1)

	//sync local registry image to dir, scoped
	imageRef, err = docker.ParseReference(fmt.Sprintf("//%s", path.Join(v2DockerRegistryURL, reference.Path(imageRef.DockerReference()))))
	require.NoError(t, err)
	imagePath = imageRef.DockerReference().String()
	assertSkopeoSucceeds(t, "", "sync", "--scoped", "--src-tls-verify=false", "--src", "docker", "--dest", "dir", path.Join(v2DockerRegistryURL, reference.Path(imageRef.DockerReference())), dir1)
	_, err = os.Stat(path.Join(dir1, imagePath, "manifest.json"))
	require.NoError(t, err)
}

func (s *syncSuite) TestDocker2DirUntagged() {
	t := s.T()
	tmpDir := t.TempDir()

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	image := pullableRepo
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	require.NoError(t, err)
	imagePath := imageRef.DockerReference().String()

	dir1 := path.Join(tmpDir, "dir1")
	assertSkopeoSucceeds(t, "", "sync", "--scoped", "--src", "docker", "--dest", "dir", image, dir1)

	sysCtx := types.SystemContext{}
	tags, err := docker.GetRepositoryTags(context.Background(), &sysCtx, imageRef)
	require.NoError(t, err)
	assert.NotZero(t, len(tags))

	nManifests, err := filepath.Glob(path.Join(dir1, path.Dir(imagePath), "*", "manifest.json"))
	require.NoError(t, err)
	assert.Len(t, nManifests, len(tags))
}

func (s *syncSuite) TestYamlUntagged() {
	t := s.T()
	tmpDir := t.TempDir()
	dir1 := path.Join(tmpDir, "dir1")

	image := pullableRepo
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	require.NoError(t, err)
	imagePath := imageRef.DockerReference().Name()

	sysCtx := types.SystemContext{}
	tags, err := docker.GetRepositoryTags(context.Background(), &sysCtx, imageRef)
	require.NoError(t, err)
	assert.NotZero(t, len(tags))

	yamlConfig := fmt.Sprintf(`
%s:
  images:
    %s: []
`, reference.Domain(imageRef.DockerReference()), reference.Path(imageRef.DockerReference()))

	// sync to the local registry
	yamlFile := path.Join(tmpDir, "registries.yaml")
	err = os.WriteFile(yamlFile, []byte(yamlConfig), 0644)
	require.NoError(t, err)
	assertSkopeoSucceeds(t, "", "sync", "--scoped", "--src", "yaml", "--dest", "docker", "--dest-tls-verify=false", yamlFile, v2DockerRegistryURL)
	// sync back from local registry to a folder
	os.Remove(yamlFile)
	yamlConfig = fmt.Sprintf(`
%s:
  tls-verify: false
  images:
    %s: []
`, v2DockerRegistryURL, imagePath)

	err = os.WriteFile(yamlFile, []byte(yamlConfig), 0644)
	require.NoError(t, err)
	assertSkopeoSucceeds(t, "", "sync", "--scoped", "--src", "yaml", "--dest", "dir", yamlFile, dir1)

	sysCtx = types.SystemContext{
		DockerInsecureSkipTLSVerify: types.NewOptionalBool(true),
	}
	localImageRef, err := docker.ParseReference(fmt.Sprintf("//%s/%s", v2DockerRegistryURL, imagePath))
	require.NoError(t, err)
	localTags, err := docker.GetRepositoryTags(context.Background(), &sysCtx, localImageRef)
	require.NoError(t, err)
	assert.NotZero(t, len(localTags))
	assert.Len(t, localTags, len(tags))
	assertNumberOfManifestsInSubdirs(t, dir1, len(tags))
}

func (s *syncSuite) TestYamlRegex2Dir() {
	t := s.T()
	tmpDir := t.TempDir()
	dir1 := path.Join(tmpDir, "dir1")

	yamlConfig := `
registry.k8s.io:
  images-by-tag-regex:
    pause: ^[12]\.0$  # regex string test
`
	// the       ↑    regex strings always matches only 2 images
	var nTags = 2
	assert.NotZero(t, nTags)

	yamlFile := path.Join(tmpDir, "registries.yaml")
	err := os.WriteFile(yamlFile, []byte(yamlConfig), 0644)
	require.NoError(t, err)
	assertSkopeoSucceeds(t, "", "sync", "--scoped", "--src", "yaml", "--dest", "dir", yamlFile, dir1)
	assertNumberOfManifestsInSubdirs(t, dir1, nTags)
}

func (s *syncSuite) TestYamlDigest2Dir() {
	t := s.T()
	tmpDir := t.TempDir()
	dir1 := path.Join(tmpDir, "dir1")

	yamlConfig := `
registry.k8s.io:
  images:
    pause:
    - sha256:59eec8837a4d942cc19a52b8c09ea75121acc38114a2c68b98983ce9356b8610
`
	yamlFile := path.Join(tmpDir, "registries.yaml")
	err := os.WriteFile(yamlFile, []byte(yamlConfig), 0644)
	require.NoError(t, err)
	assertSkopeoSucceeds(t, "", "sync", "--scoped", "--src", "yaml", "--dest", "dir", yamlFile, dir1)
	assertNumberOfManifestsInSubdirs(t, dir1, 1)
}

func (s *syncSuite) TestYaml2Dir() {
	t := s.T()
	tmpDir := t.TempDir()
	dir1 := path.Join(tmpDir, "dir1")

	yamlConfig := `
registry.k8s.io:
  images:
    coredns/coredns:
      - v1.8.0
      - v1.7.1
    k8s-dns-kube-dns:
      - 1.14.12
      - 1.14.13
    pause:
      - latest

quay.io:
  images:
    quay/busybox:
      - latest`

	// get the number of tags
	re := regexp.MustCompile(`^ +- +[^:/ ]+`)
	var nTags int
	for _, l := range strings.Split(yamlConfig, "\n") {
		if re.MatchString(l) {
			nTags++
		}
	}
	assert.NotZero(t, nTags)

	yamlFile := path.Join(tmpDir, "registries.yaml")
	err := os.WriteFile(yamlFile, []byte(yamlConfig), 0644)
	require.NoError(t, err)
	assertSkopeoSucceeds(t, "", "sync", "--scoped", "--src", "yaml", "--dest", "dir", yamlFile, dir1)
	assertNumberOfManifestsInSubdirs(t, dir1, nTags)
}

func (s *syncSuite) TestYamlTLSVerify() {
	t := s.T()
	const localRegURL = "docker://" + v2DockerRegistryURL + "/"
	tmpDir := t.TempDir()
	dir1 := path.Join(tmpDir, "dir1")
	image := pullableRepoWithLatestTag
	tag := "latest"

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	// copy docker => docker
	assertSkopeoSucceeds(t, "", "copy", "--dest-tls-verify=false", "docker://"+image+":"+tag, localRegURL+image+":"+tag)

	yamlTemplate := `
%s:
  %s
  images:
    %s:
      - %s`

	testCfg := []struct {
		tlsVerify string
		msg       string
		checker   func(t *testing.T, regexp string, args ...string)
	}{
		{
			tlsVerify: "tls-verify: false",
			msg:       "",
			checker:   assertSkopeoSucceeds,
		},
		{
			tlsVerify: "tls-verify: true",
			msg:       ".*server gave HTTP response to HTTPS client.*",
			checker:   assertSkopeoFails,
		},
		// no "tls-verify" line means default TLS verify must be ON
		{
			tlsVerify: "",
			msg:       ".*server gave HTTP response to HTTPS client.*",
			checker:   assertSkopeoFails,
		},
	}

	for _, cfg := range testCfg {
		yamlConfig := fmt.Sprintf(yamlTemplate, v2DockerRegistryURL, cfg.tlsVerify, image, tag)
		yamlFile := path.Join(tmpDir, "registries.yaml")
		err := os.WriteFile(yamlFile, []byte(yamlConfig), 0644)
		require.NoError(t, err)

		cfg.checker(t, cfg.msg, "sync", "--scoped", "--src", "yaml", "--dest", "dir", yamlFile, dir1)
		os.Remove(yamlFile)
		os.RemoveAll(dir1)
	}

}

func (s *syncSuite) TestSyncManifestOutput() {
	t := s.T()
	tmpDir := t.TempDir()

	destDir1 := filepath.Join(tmpDir, "dest1")
	destDir2 := filepath.Join(tmpDir, "dest2")
	destDir3 := filepath.Join(tmpDir, "dest3")

	//Split image:tag path from image URI for manifest comparison
	imageDir := pullableTaggedImage[strings.LastIndex(pullableTaggedImage, "/")+1:]

	assertSkopeoSucceeds(t, "", "sync", "--format=oci", "--all", "--src", "docker", "--dest", "dir", pullableTaggedImage, destDir1)
	verifyManifestMIMEType(t, filepath.Join(destDir1, imageDir), imgspecv1.MediaTypeImageManifest)
	assertSkopeoSucceeds(t, "", "sync", "--format=v2s2", "--all", "--src", "docker", "--dest", "dir", pullableTaggedImage, destDir2)
	verifyManifestMIMEType(t, filepath.Join(destDir2, imageDir), manifest.DockerV2Schema2MediaType)
	assertSkopeoSucceeds(t, "", "sync", "--format=v2s1", "--all", "--src", "docker", "--dest", "dir", pullableTaggedImage, destDir3)
	verifyManifestMIMEType(t, filepath.Join(destDir3, imageDir), manifest.DockerV2Schema1SignedMediaType)
}

func (s *syncSuite) TestDocker2DockerTagged() {
	t := s.T()
	const localRegURL = "docker://" + v2DockerRegistryURL + "/"

	tmpDir := t.TempDir()

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	image := pullableTaggedImage
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	require.NoError(t, err)
	imagePath := imageRef.DockerReference().String()

	dir1 := path.Join(tmpDir, "dir1")
	dir2 := path.Join(tmpDir, "dir2")

	// sync docker => docker
	assertSkopeoSucceeds(t, "", "sync", "--scoped", "--dest-tls-verify=false", "--src", "docker", "--dest", "docker", image, v2DockerRegistryURL)

	// copy docker => dir
	assertSkopeoSucceeds(t, "", "copy", "docker://"+image, "dir:"+dir1)
	_, err = os.Stat(path.Join(dir1, "manifest.json"))
	require.NoError(t, err)

	// copy docker => dir
	assertSkopeoSucceeds(t, "", "copy", "--src-tls-verify=false", localRegURL+imagePath, "dir:"+dir2)
	_, err = os.Stat(path.Join(dir2, "manifest.json"))
	require.NoError(t, err)

	out := combinedOutputOfCommand(t, "diff", "-urN", dir1, dir2)
	assert.Equal(t, "", out)
}

func (s *syncSuite) TestDir2DockerTagged() {
	t := s.T()
	const localRegURL = "docker://" + v2DockerRegistryURL + "/"

	tmpDir := t.TempDir()

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	image := pullableRepoWithLatestTag

	dir1 := path.Join(tmpDir, "dir1")
	err := os.Mkdir(dir1, 0755)
	require.NoError(t, err)
	dir2 := path.Join(tmpDir, "dir2")
	err = os.Mkdir(dir2, 0755)
	require.NoError(t, err)

	// create leading dirs
	err = os.MkdirAll(path.Dir(path.Join(dir1, image)), 0755)
	require.NoError(t, err)

	// copy docker => dir
	assertSkopeoSucceeds(t, "", "copy", "docker://"+image, "dir:"+path.Join(dir1, image))
	_, err = os.Stat(path.Join(dir1, image, "manifest.json"))
	require.NoError(t, err)

	// sync dir => docker
	assertSkopeoSucceeds(t, "", "sync", "--scoped", "--dest-tls-verify=false", "--src", "dir", "--dest", "docker", dir1, v2DockerRegistryURL)

	// create leading dirs
	err = os.MkdirAll(path.Dir(path.Join(dir2, image)), 0755)
	require.NoError(t, err)

	// copy docker => dir
	assertSkopeoSucceeds(t, "", "copy", "--src-tls-verify=false", localRegURL+image, "dir:"+path.Join(dir2, image))
	_, err = os.Stat(path.Join(dir2, image, "manifest.json"))
	require.NoError(t, err)

	out := combinedOutputOfCommand(t, "diff", "-urN", dir1, dir2)
	assert.Equal(t, "", out)
}

func (s *syncSuite) TestFailsWithDir2Dir() {
	t := s.T()
	tmpDir := t.TempDir()

	dir1 := path.Join(tmpDir, "dir1")
	dir2 := path.Join(tmpDir, "dir2")

	// sync dir => dir is not allowed
	assertSkopeoFails(t, ".*sync from 'dir' to 'dir' not implemented.*", "sync", "--scoped", "--src", "dir", "--dest", "dir", dir1, dir2)
}

func (s *syncSuite) TestFailsNoSourceImages() {
	t := s.T()
	tmpDir := t.TempDir()

	assertSkopeoFails(t, ".*No images to sync found in .*",
		"sync", "--scoped", "--dest-tls-verify=false", "--src", "dir", "--dest", "docker", tmpDir, v2DockerRegistryURL)

	assertSkopeoFails(t, ".*Error determining repository tags for repo docker.io/library/hopefully_no_images_will_ever_be_called_like_this: fetching tags list: requested access to the resource is denied.*",
		"sync", "--scoped", "--dest-tls-verify=false", "--src", "docker", "--dest", "docker", "hopefully_no_images_will_ever_be_called_like_this", v2DockerRegistryURL)
}

func (s *syncSuite) TestFailsWithDockerSourceNoRegistry() {
	t := s.T()
	const regURL = "google.com/namespace/imagename"

	tmpDir := t.TempDir()

	//untagged
	assertSkopeoFails(t, ".*StatusCode: 404.*",
		"sync", "--scoped", "--src", "docker", "--dest", "dir", regURL, tmpDir)

	//tagged
	assertSkopeoFails(t, ".*StatusCode: 404.*",
		"sync", "--scoped", "--src", "docker", "--dest", "dir", regURL+":thetag", tmpDir)
}

func (s *syncSuite) TestFailsWithDockerSourceUnauthorized() {
	t := s.T()
	const repo = "privateimagenamethatshouldnotbepublic"
	tmpDir := t.TempDir()

	//untagged
	assertSkopeoFails(t, ".*requested access to the resource is denied.*",
		"sync", "--scoped", "--src", "docker", "--dest", "dir", repo, tmpDir)

	//tagged
	assertSkopeoFails(t, ".*requested access to the resource is denied.*",
		"sync", "--scoped", "--src", "docker", "--dest", "dir", repo+":thetag", tmpDir)
}

func (s *syncSuite) TestFailsWithDockerSourceNotExisting() {
	t := s.T()
	repo := path.Join(v2DockerRegistryURL, "imagedoesnotexist")
	tmpDir := t.TempDir()

	//untagged
	assertSkopeoFails(t, ".*repository name not known to registry.*",
		"sync", "--scoped", "--src-tls-verify=false", "--src", "docker", "--dest", "dir", repo, tmpDir)

	//tagged
	assertSkopeoFails(t, ".*reading manifest.*",
		"sync", "--scoped", "--src-tls-verify=false", "--src", "docker", "--dest", "dir", repo+":thetag", tmpDir)
}

func (s *syncSuite) TestFailsWithDirSourceNotExisting() {
	t := s.T()
	// Make sure the dir does not exist!
	tmpDir := t.TempDir()
	tmpDir = filepath.Join(tmpDir, "this-does-not-exist")
	err := os.RemoveAll(tmpDir)
	require.NoError(t, err)
	_, err = os.Stat(path.Join(tmpDir))
	assert.True(t, os.IsNotExist(err))

	assertSkopeoFails(t, ".*no such file or directory.*",
		"sync", "--scoped", "--dest-tls-verify=false", "--src", "dir", "--dest", "docker", tmpDir, v2DockerRegistryURL)
}
