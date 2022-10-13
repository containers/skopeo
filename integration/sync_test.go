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

	"github.com/containers/image/v5/docker"
	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/types"
	"github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"gopkg.in/check.v1"
)

const (
	// A repository with a path with multiple components in it which
	// contains multiple tags, preferably with some tags pointing to
	// manifest lists, and with some tags that don't.
	pullableRepo = "k8s.gcr.io/coredns/coredns"
	// A tagged image in the repository that we can inspect and copy.
	pullableTaggedImage = "k8s.gcr.io/coredns/coredns:v1.6.6"
	// A tagged manifest list in the repository that we can inspect and copy.
	pullableTaggedManifestList = "k8s.gcr.io/coredns/coredns:v1.8.0"
	// A repository containing multiple tags, some of which are for
	// manifest lists, and which includes a "latest" tag.  We specify the
	// name here without a tag.
	pullableRepoWithLatestTag = "k8s.gcr.io/pause"
)

func init() {
	check.Suite(&SyncSuite{})
}

type SyncSuite struct {
	cluster  *openshiftCluster
	registry *testRegistryV2
}

func (s *SyncSuite) SetUpSuite(c *check.C) {
	const registryAuth = false
	const registrySchema1 = false

	if os.Getenv("SKOPEO_LOCAL_TESTS") == "1" {
		c.Log("Running tests without a container")
		fmt.Printf("NOTE: tests requires a V2 registry at url=%s, with auth=%t, schema1=%t \n", v2DockerRegistryURL, registryAuth, registrySchema1)
		return
	}

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
	s.registry = setupRegistryV2At(c, v2DockerRegistryURL, registryAuth, registrySchema1)

	gpgHome := c.MkDir()
	os.Setenv("GNUPGHOME", gpgHome)

	for _, key := range []string{"personal", "official"} {
		batchInput := fmt.Sprintf("Key-Type: RSA\nName-Real: Test key - %s\nName-email: %s@example.com\n%%no-protection\n%%commit\n",
			key, key)
		runCommandWithInput(c, batchInput, gpgBinary, "--batch", "--gen-key")

		out := combinedOutputOfCommand(c, gpgBinary, "--armor", "--export", fmt.Sprintf("%s@example.com", key))
		err := os.WriteFile(filepath.Join(gpgHome, fmt.Sprintf("%s-pubkey.gpg", key)),
			[]byte(out), 0600)
		c.Assert(err, check.IsNil)
	}
}

func (s *SyncSuite) TearDownSuite(c *check.C) {
	if os.Getenv("SKOPEO_LOCAL_TESTS") == "1" {
		return
	}

	if s.registry != nil {
		s.registry.tearDown(c)
	}
	if s.cluster != nil {
		s.cluster.tearDown(c)
	}
}

func assertNumberOfManifestsInSubdirs(c *check.C, dir string, expectedCount int) {
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
	c.Assert(err, check.IsNil)
	c.Assert(nManifests, check.Equals, expectedCount)
}

func (s *SyncSuite) TestDocker2DirTagged(c *check.C) {
	tmpDir := c.MkDir()

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	image := pullableTaggedImage
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	c.Assert(err, check.IsNil)
	imagePath := imageRef.DockerReference().String()

	dir1 := path.Join(tmpDir, "dir1")
	dir2 := path.Join(tmpDir, "dir2")

	// sync docker => dir
	assertSkopeoSucceeds(c, "", "sync", "--scoped", "--src", "docker", "--dest", "dir", image, dir1)
	_, err = os.Stat(path.Join(dir1, imagePath, "manifest.json"))
	c.Assert(err, check.IsNil)

	// copy docker => dir
	assertSkopeoSucceeds(c, "", "copy", "docker://"+image, "dir:"+dir2)
	_, err = os.Stat(path.Join(dir2, "manifest.json"))
	c.Assert(err, check.IsNil)

	out := combinedOutputOfCommand(c, "diff", "-urN", path.Join(dir1, imagePath), dir2)
	c.Assert(out, check.Equals, "")
}

func (s *SyncSuite) TestDocker2DirTaggedAll(c *check.C) {
	tmpDir := c.MkDir()

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	image := pullableTaggedManifestList
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	c.Assert(err, check.IsNil)
	imagePath := imageRef.DockerReference().String()

	dir1 := path.Join(tmpDir, "dir1")
	dir2 := path.Join(tmpDir, "dir2")

	// sync docker => dir
	assertSkopeoSucceeds(c, "", "sync", "--all", "--scoped", "--src", "docker", "--dest", "dir", image, dir1)
	_, err = os.Stat(path.Join(dir1, imagePath, "manifest.json"))
	c.Assert(err, check.IsNil)

	// copy docker => dir
	assertSkopeoSucceeds(c, "", "copy", "--all", "docker://"+image, "dir:"+dir2)
	_, err = os.Stat(path.Join(dir2, "manifest.json"))
	c.Assert(err, check.IsNil)

	out := combinedOutputOfCommand(c, "diff", "-urN", path.Join(dir1, imagePath), dir2)
	c.Assert(out, check.Equals, "")
}

func (s *SyncSuite) TestPreserveDigests(c *check.C) {
	tmpDir := c.MkDir()

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	image := pullableTaggedManifestList

	// copy docker => dir
	assertSkopeoSucceeds(c, "", "copy", "--all", "--preserve-digests", "docker://"+image, "dir:"+tmpDir)
	_, err := os.Stat(path.Join(tmpDir, "manifest.json"))
	c.Assert(err, check.IsNil)

	assertSkopeoFails(c, ".*Instructed to preserve digests.*", "copy", "--all", "--preserve-digests", "--format=oci", "docker://"+image, "dir:"+tmpDir)
}

func (s *SyncSuite) TestScoped(c *check.C) {
	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	image := pullableTaggedImage
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	c.Assert(err, check.IsNil)
	imagePath := imageRef.DockerReference().String()

	dir1 := c.MkDir()
	assertSkopeoSucceeds(c, "", "sync", "--src", "docker", "--dest", "dir", image, dir1)
	_, err = os.Stat(path.Join(dir1, path.Base(imagePath), "manifest.json"))
	c.Assert(err, check.IsNil)

	assertSkopeoSucceeds(c, "", "sync", "--scoped", "--src", "docker", "--dest", "dir", image, dir1)
	_, err = os.Stat(path.Join(dir1, imagePath, "manifest.json"))
	c.Assert(err, check.IsNil)
}

func (s *SyncSuite) TestDirIsNotOverwritten(c *check.C) {
	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	image := pullableRepoWithLatestTag
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	c.Assert(err, check.IsNil)
	imagePath := imageRef.DockerReference().String()

	// make a copy of the image in the local registry
	assertSkopeoSucceeds(c, "", "copy", "--dest-tls-verify=false", "docker://"+image, "docker://"+path.Join(v2DockerRegistryURL, reference.Path(imageRef.DockerReference())))

	//sync upstream image to dir, not scoped
	dir1 := c.MkDir()
	assertSkopeoSucceeds(c, "", "sync", "--src", "docker", "--dest", "dir", image, dir1)
	_, err = os.Stat(path.Join(dir1, path.Base(imagePath), "manifest.json"))
	c.Assert(err, check.IsNil)

	//sync local registry image to dir, not scoped
	assertSkopeoFails(c, ".*Refusing to overwrite destination directory.*", "sync", "--src-tls-verify=false", "--src", "docker", "--dest", "dir", path.Join(v2DockerRegistryURL, reference.Path(imageRef.DockerReference())), dir1)

	//sync local registry image to dir, scoped
	imageRef, err = docker.ParseReference(fmt.Sprintf("//%s", path.Join(v2DockerRegistryURL, reference.Path(imageRef.DockerReference()))))
	c.Assert(err, check.IsNil)
	imagePath = imageRef.DockerReference().String()
	assertSkopeoSucceeds(c, "", "sync", "--scoped", "--src-tls-verify=false", "--src", "docker", "--dest", "dir", path.Join(v2DockerRegistryURL, reference.Path(imageRef.DockerReference())), dir1)
	_, err = os.Stat(path.Join(dir1, imagePath, "manifest.json"))
	c.Assert(err, check.IsNil)
}

func (s *SyncSuite) TestDocker2DirUntagged(c *check.C) {
	tmpDir := c.MkDir()

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	image := pullableRepo
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	c.Assert(err, check.IsNil)
	imagePath := imageRef.DockerReference().String()

	dir1 := path.Join(tmpDir, "dir1")
	assertSkopeoSucceeds(c, "", "sync", "--scoped", "--src", "docker", "--dest", "dir", image, dir1)

	sysCtx := types.SystemContext{}
	tags, err := docker.GetRepositoryTags(context.Background(), &sysCtx, imageRef)
	c.Assert(err, check.IsNil)
	c.Check(len(tags), check.Not(check.Equals), 0)

	nManifests, err := filepath.Glob(path.Join(dir1, path.Dir(imagePath), "*", "manifest.json"))
	c.Assert(err, check.IsNil)
	c.Assert(len(nManifests), check.Equals, len(tags))
}

func (s *SyncSuite) TestYamlUntagged(c *check.C) {
	tmpDir := c.MkDir()
	dir1 := path.Join(tmpDir, "dir1")

	image := pullableRepo
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	c.Assert(err, check.IsNil)
	imagePath := imageRef.DockerReference().Name()

	sysCtx := types.SystemContext{}
	tags, err := docker.GetRepositoryTags(context.Background(), &sysCtx, imageRef)
	c.Assert(err, check.IsNil)
	c.Check(len(tags), check.Not(check.Equals), 0)

	yamlConfig := fmt.Sprintf(`
%s:
  images:
    %s: []
`, reference.Domain(imageRef.DockerReference()), reference.Path(imageRef.DockerReference()))

	// sync to the local registry
	yamlFile := path.Join(tmpDir, "registries.yaml")
	err = os.WriteFile(yamlFile, []byte(yamlConfig), 0644)
	c.Assert(err, check.IsNil)
	assertSkopeoSucceeds(c, "", "sync", "--scoped", "--src", "yaml", "--dest", "docker", "--dest-tls-verify=false", yamlFile, v2DockerRegistryURL)
	// sync back from local registry to a folder
	os.Remove(yamlFile)
	yamlConfig = fmt.Sprintf(`
%s:
  tls-verify: false
  images:
    %s: []
`, v2DockerRegistryURL, imagePath)

	err = os.WriteFile(yamlFile, []byte(yamlConfig), 0644)
	c.Assert(err, check.IsNil)
	assertSkopeoSucceeds(c, "", "sync", "--scoped", "--src", "yaml", "--dest", "dir", yamlFile, dir1)

	sysCtx = types.SystemContext{
		DockerInsecureSkipTLSVerify: types.NewOptionalBool(true),
	}
	localImageRef, err := docker.ParseReference(fmt.Sprintf("//%s/%s", v2DockerRegistryURL, imagePath))
	c.Assert(err, check.IsNil)
	localTags, err := docker.GetRepositoryTags(context.Background(), &sysCtx, localImageRef)
	c.Assert(err, check.IsNil)
	c.Check(len(localTags), check.Not(check.Equals), 0)
	c.Assert(len(localTags), check.Equals, len(tags))
	assertNumberOfManifestsInSubdirs(c, dir1, len(tags))
}

func (s *SyncSuite) TestYamlRegex2Dir(c *check.C) {
	tmpDir := c.MkDir()
	dir1 := path.Join(tmpDir, "dir1")

	yamlConfig := `
k8s.gcr.io:
  images-by-tag-regex:
    pause: ^[12]\.0$  # regex string test
`
	// the       ↑    regex strings always matches only 2 images
	var nTags = 2
	c.Assert(nTags, check.Not(check.Equals), 0)

	yamlFile := path.Join(tmpDir, "registries.yaml")
	err := os.WriteFile(yamlFile, []byte(yamlConfig), 0644)
	c.Assert(err, check.IsNil)
	assertSkopeoSucceeds(c, "", "sync", "--scoped", "--src", "yaml", "--dest", "dir", yamlFile, dir1)
	assertNumberOfManifestsInSubdirs(c, dir1, nTags)
}

func (s *SyncSuite) TestYamlDigest2Dir(c *check.C) {
	tmpDir := c.MkDir()
	dir1 := path.Join(tmpDir, "dir1")

	yamlConfig := `
k8s.gcr.io:
  images:
    pause:
    - sha256:59eec8837a4d942cc19a52b8c09ea75121acc38114a2c68b98983ce9356b8610
`
	yamlFile := path.Join(tmpDir, "registries.yaml")
	err := os.WriteFile(yamlFile, []byte(yamlConfig), 0644)
	c.Assert(err, check.IsNil)
	assertSkopeoSucceeds(c, "", "sync", "--scoped", "--src", "yaml", "--dest", "dir", yamlFile, dir1)
	assertNumberOfManifestsInSubdirs(c, dir1, 1)
}

func (s *SyncSuite) TestYaml2Dir(c *check.C) {
	tmpDir := c.MkDir()
	dir1 := path.Join(tmpDir, "dir1")

	yamlConfig := `
k8s.gcr.io:
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
	c.Assert(nTags, check.Not(check.Equals), 0)

	yamlFile := path.Join(tmpDir, "registries.yaml")
	err := os.WriteFile(yamlFile, []byte(yamlConfig), 0644)
	c.Assert(err, check.IsNil)
	assertSkopeoSucceeds(c, "", "sync", "--scoped", "--src", "yaml", "--dest", "dir", yamlFile, dir1)
	assertNumberOfManifestsInSubdirs(c, dir1, nTags)
}

func (s *SyncSuite) TestYamlTLSVerify(c *check.C) {
	const localRegURL = "docker://" + v2DockerRegistryURL + "/"
	tmpDir := c.MkDir()
	dir1 := path.Join(tmpDir, "dir1")
	image := pullableRepoWithLatestTag
	tag := "latest"

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	// copy docker => docker
	assertSkopeoSucceeds(c, "", "copy", "--dest-tls-verify=false", "docker://"+image+":"+tag, localRegURL+image+":"+tag)

	yamlTemplate := `
%s:
  %s
  images:
    %s:
      - %s`

	testCfg := []struct {
		tlsVerify string
		msg       string
		checker   func(c *check.C, regexp string, args ...string)
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
		c.Assert(err, check.IsNil)

		cfg.checker(c, cfg.msg, "sync", "--scoped", "--src", "yaml", "--dest", "dir", yamlFile, dir1)
		os.Remove(yamlFile)
		os.RemoveAll(dir1)
	}

}

func (s *SyncSuite) TestSyncManifestOutput(c *check.C) {
	tmpDir := c.MkDir()

	destDir1 := filepath.Join(tmpDir, "dest1")
	destDir2 := filepath.Join(tmpDir, "dest2")
	destDir3 := filepath.Join(tmpDir, "dest3")

	//Split image:tag path from image URI for manifest comparison
	imageDir := pullableTaggedImage[strings.LastIndex(pullableTaggedImage, "/")+1:]

	assertSkopeoSucceeds(c, "", "sync", "--format=oci", "--all", "--src", "docker", "--dest", "dir", pullableTaggedImage, destDir1)
	verifyManifestMIMEType(c, filepath.Join(destDir1, imageDir), imgspecv1.MediaTypeImageManifest)
	assertSkopeoSucceeds(c, "", "sync", "--format=v2s2", "--all", "--src", "docker", "--dest", "dir", pullableTaggedImage, destDir2)
	verifyManifestMIMEType(c, filepath.Join(destDir2, imageDir), manifest.DockerV2Schema2MediaType)
	assertSkopeoSucceeds(c, "", "sync", "--format=v2s1", "--all", "--src", "docker", "--dest", "dir", pullableTaggedImage, destDir3)
	verifyManifestMIMEType(c, filepath.Join(destDir3, imageDir), manifest.DockerV2Schema1SignedMediaType)
}

func (s *SyncSuite) TestDocker2DockerTagged(c *check.C) {
	const localRegURL = "docker://" + v2DockerRegistryURL + "/"

	tmpDir := c.MkDir()

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	image := pullableTaggedImage
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	c.Assert(err, check.IsNil)
	imagePath := imageRef.DockerReference().String()

	dir1 := path.Join(tmpDir, "dir1")
	dir2 := path.Join(tmpDir, "dir2")

	// sync docker => docker
	assertSkopeoSucceeds(c, "", "sync", "--scoped", "--dest-tls-verify=false", "--src", "docker", "--dest", "docker", image, v2DockerRegistryURL)

	// copy docker => dir
	assertSkopeoSucceeds(c, "", "copy", "docker://"+image, "dir:"+dir1)
	_, err = os.Stat(path.Join(dir1, "manifest.json"))
	c.Assert(err, check.IsNil)

	// copy docker => dir
	assertSkopeoSucceeds(c, "", "copy", "--src-tls-verify=false", localRegURL+imagePath, "dir:"+dir2)
	_, err = os.Stat(path.Join(dir2, "manifest.json"))
	c.Assert(err, check.IsNil)

	out := combinedOutputOfCommand(c, "diff", "-urN", dir1, dir2)
	c.Assert(out, check.Equals, "")
}

func (s *SyncSuite) TestDir2DockerTagged(c *check.C) {
	const localRegURL = "docker://" + v2DockerRegistryURL + "/"

	tmpDir := c.MkDir()

	// FIXME: It would be nice to use one of the local Docker registries instead of needing an Internet connection.
	image := pullableRepoWithLatestTag

	dir1 := path.Join(tmpDir, "dir1")
	err := os.Mkdir(dir1, 0755)
	c.Assert(err, check.IsNil)
	dir2 := path.Join(tmpDir, "dir2")
	err = os.Mkdir(dir2, 0755)
	c.Assert(err, check.IsNil)

	// create leading dirs
	err = os.MkdirAll(path.Dir(path.Join(dir1, image)), 0755)
	c.Assert(err, check.IsNil)

	// copy docker => dir
	assertSkopeoSucceeds(c, "", "copy", "docker://"+image, "dir:"+path.Join(dir1, image))
	_, err = os.Stat(path.Join(dir1, image, "manifest.json"))
	c.Assert(err, check.IsNil)

	// sync dir => docker
	assertSkopeoSucceeds(c, "", "sync", "--scoped", "--dest-tls-verify=false", "--src", "dir", "--dest", "docker", dir1, v2DockerRegistryURL)

	// create leading dirs
	err = os.MkdirAll(path.Dir(path.Join(dir2, image)), 0755)
	c.Assert(err, check.IsNil)

	// copy docker => dir
	assertSkopeoSucceeds(c, "", "copy", "--src-tls-verify=false", localRegURL+image, "dir:"+path.Join(dir2, image))
	_, err = os.Stat(path.Join(dir2, image, "manifest.json"))
	c.Assert(err, check.IsNil)

	out := combinedOutputOfCommand(c, "diff", "-urN", dir1, dir2)
	c.Assert(out, check.Equals, "")
}

func (s *SyncSuite) TestFailsWithDir2Dir(c *check.C) {
	tmpDir := c.MkDir()

	dir1 := path.Join(tmpDir, "dir1")
	dir2 := path.Join(tmpDir, "dir2")

	// sync dir => dir is not allowed
	assertSkopeoFails(c, ".*sync from 'dir' to 'dir' not implemented.*", "sync", "--scoped", "--src", "dir", "--dest", "dir", dir1, dir2)
}

func (s *SyncSuite) TestFailsNoSourceImages(c *check.C) {
	tmpDir := c.MkDir()

	assertSkopeoFails(c, ".*No images to sync found in .*",
		"sync", "--scoped", "--dest-tls-verify=false", "--src", "dir", "--dest", "docker", tmpDir, v2DockerRegistryURL)

	assertSkopeoFails(c, ".*Error determining repository tags for repo docker.io/library/hopefully_no_images_will_ever_be_called_like_this: fetching tags list: requested access to the resource is denied.*",
		"sync", "--scoped", "--dest-tls-verify=false", "--src", "docker", "--dest", "docker", "hopefully_no_images_will_ever_be_called_like_this", v2DockerRegistryURL)
}

func (s *SyncSuite) TestFailsWithDockerSourceNoRegistry(c *check.C) {
	const regURL = "google.com/namespace/imagename"

	tmpDir := c.MkDir()

	//untagged
	assertSkopeoFails(c, ".*StatusCode: 404.*",
		"sync", "--scoped", "--src", "docker", "--dest", "dir", regURL, tmpDir)

	//tagged
	assertSkopeoFails(c, ".*StatusCode: 404.*",
		"sync", "--scoped", "--src", "docker", "--dest", "dir", regURL+":thetag", tmpDir)
}

func (s *SyncSuite) TestFailsWithDockerSourceUnauthorized(c *check.C) {
	const repo = "privateimagenamethatshouldnotbepublic"
	tmpDir := c.MkDir()

	//untagged
	assertSkopeoFails(c, ".*requested access to the resource is denied.*",
		"sync", "--scoped", "--src", "docker", "--dest", "dir", repo, tmpDir)

	//tagged
	assertSkopeoFails(c, ".*requested access to the resource is denied.*",
		"sync", "--scoped", "--src", "docker", "--dest", "dir", repo+":thetag", tmpDir)
}

func (s *SyncSuite) TestFailsWithDockerSourceNotExisting(c *check.C) {
	repo := path.Join(v2DockerRegistryURL, "imagedoesnotexist")
	tmpDir := c.MkDir()

	//untagged
	assertSkopeoFails(c, ".*repository name not known to registry.*",
		"sync", "--scoped", "--src-tls-verify=false", "--src", "docker", "--dest", "dir", repo, tmpDir)

	//tagged
	assertSkopeoFails(c, ".*reading manifest.*",
		"sync", "--scoped", "--src-tls-verify=false", "--src", "docker", "--dest", "dir", repo+":thetag", tmpDir)
}

func (s *SyncSuite) TestFailsWithDirSourceNotExisting(c *check.C) {
	// Make sure the dir does not exist!
	tmpDir := c.MkDir()
	tmpDir = filepath.Join(tmpDir, "this-does-not-exist")
	err := os.RemoveAll(tmpDir)
	c.Assert(err, check.IsNil)
	_, err = os.Stat(path.Join(tmpDir))
	c.Check(os.IsNotExist(err), check.Equals, true)

	assertSkopeoFails(c, ".*no such file or directory.*",
		"sync", "--scoped", "--dest-tls-verify=false", "--src", "dir", "--dest", "docker", tmpDir, v2DockerRegistryURL)
}

func (s *SyncSuite) TestYamlV2TaggedTags(c *check.C) {
	image := pullableRepo
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	c.Assert(err, check.IsNil)

	tag1 := "v1.8.0"
	tag2 := "v1.7.1"

	yamlConfig := fmt.Sprintf(`
%s:
  repos:
    %s:
      destination-repo: %s
      mirror-tags:
      - %s
      - %s`, reference.Domain(imageRef.DockerReference()), reference.Path(imageRef.DockerReference()), reference.Path(imageRef.DockerReference()), tag1, tag2)

	// sync to the local registry
	tmpDir := c.MkDir()
	yamlFile := path.Join(tmpDir, "registries.yaml")
	err = os.WriteFile(yamlFile, []byte(yamlConfig), 0644)
	c.Assert(err, check.IsNil)
	assertSkopeoSucceeds(c, "", "sync", "--scoped", "--src", "yaml2", "--dest", "docker", "--dest-tls-verify=false", yamlFile, v2DockerRegistryURL)

	localImageRef, err := docker.ParseReference(fmt.Sprintf("//%s/%s", v2DockerRegistryURL, reference.Path(imageRef.DockerReference())))
	c.Assert(err, check.IsNil)

	sysCtx := types.SystemContext{
		DockerInsecureSkipTLSVerify: types.NewOptionalBool(true),
	}
	localTags, err := docker.GetRepositoryTags(context.Background(), &sysCtx, localImageRef)
	c.Assert(err, check.IsNil)
	c.Assert(len(localTags), check.Equals, 2)
	c.Assert(localTags, check.Not(check.Equals), 0)

	contains := func(val string, list []string) (_ bool) {
		for _, l := range list {
			if l == val {
				return true
			}
		}
		return
	}
	c.Assert(contains(tag1, localTags), check.Equals, true)
	c.Assert(contains(tag2, localTags), check.Equals, true)
}

func (s *SyncSuite) TestYamlV2TaggedWillSyncAll(c *check.C) {
	image := pullableRepo
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	c.Assert(err, check.IsNil)

	sysCtx := types.SystemContext{}
	tags, err := docker.GetRepositoryTags(context.Background(), &sysCtx, imageRef)
	c.Assert(err, check.IsNil)
	c.Check(len(tags), check.Not(check.Equals), 0)

	yamlConfig := fmt.Sprintf(`
%s:
  repos:
    %s:
      destination-repo: %s
`, reference.Domain(imageRef.DockerReference()), reference.Path(imageRef.DockerReference()), reference.Path(imageRef.DockerReference()))

	tmpDir := c.MkDir()
	yamlFile := path.Join(tmpDir, "registries.yaml")
	err = os.WriteFile(yamlFile, []byte(yamlConfig), 0644)
	c.Assert(err, check.IsNil)
	assertSkopeoSucceeds(c, "", "sync", "--scoped", "--src", "yaml2", "--dest", "docker", "--dest-tls-verify=false", yamlFile, v2DockerRegistryURL)

	localImageRef, err := docker.ParseReference(fmt.Sprintf("//%s/%s", v2DockerRegistryURL, reference.Path(imageRef.DockerReference())))
	c.Assert(err, check.IsNil)

	sysCtx = types.SystemContext{
		DockerInsecureSkipTLSVerify: types.NewOptionalBool(true),
	}
	localTags, err := docker.GetRepositoryTags(context.Background(), &sysCtx, localImageRef)
	c.Assert(err, check.IsNil)
	c.Assert(len(localTags), check.Equals, len(tags))
}

func (s *SyncSuite) TestYamlV2TaggedDigest(c *check.C) {
	image := pullableRepoWithLatestTag
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	c.Assert(err, check.IsNil)

	srcDomain := reference.Domain(imageRef.DockerReference())
	imagePath := reference.Path(imageRef.DockerReference())

	srcDigestStr := "sha256:59eec8837a4d942cc19a52b8c09ea75121acc38114a2c68b98983ce9356b8610"
	imageDigest, err := digest.Parse(srcDigestStr)
	c.Assert(err, check.IsNil)

	yamlConfig := fmt.Sprintf(`
%s:
  repos:
    %s:
      destination-repo: %s
      mirror-digests:
      - %s`, srcDomain, imagePath, imagePath, srcDigestStr)

	tmpDir := c.MkDir()
	yamlFile := path.Join(tmpDir, "registries.yaml")
	err = os.WriteFile(yamlFile, []byte(yamlConfig), 0644)
	c.Assert(err, check.IsNil)

	assertSkopeoSucceeds(c, "", "sync", "--scoped", "--src", "yaml2", "--dest", "docker", "--dest-tls-verify=false", yamlFile, v2DockerRegistryURL)

	localNamedRef, err := reference.ParseNormalizedNamed(fmt.Sprintf("%s/%s", v2DockerRegistryURL, imagePath))
	c.Assert(err, check.IsNil)

	localDigestedRef, err := reference.WithDigest(localNamedRef, imageDigest)
	c.Assert(err, check.IsNil)

	localImageRef, err := docker.ParseReference(fmt.Sprintf("//%s", localDigestedRef.String()))
	c.Assert(err, check.IsNil)

	sysCtx := types.SystemContext{
		DockerInsecureSkipTLSVerify: types.NewOptionalBool(true),
	}
	localDigests, err := docker.GetDigest(context.Background(), &sysCtx, localImageRef)
	c.Assert(err, check.IsNil)
	c.Assert(imageDigest.String(), check.Equals, localDigests.String())
}

func (s *SyncSuite) TestYamlV2TaggedTagRegex(c *check.C) {
	image := "gcr.io/google-containers/busybox"
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	c.Assert(err, check.IsNil)

	srcDomain := reference.Domain(imageRef.DockerReference())
	imagePath := reference.Path(imageRef.DockerReference())

	yamlConfig := fmt.Sprintf(`
%s:
  repos: 
    %s:
      destination-repo: %s
      mirror-by-tag-regex: ^1\.2[0-9]$
`, srcDomain, imagePath, imagePath)

	tmpDir := c.MkDir()
	yamlFile := path.Join(tmpDir, "registries.yaml")
	err = os.WriteFile(yamlFile, []byte(yamlConfig), 0644)
	c.Assert(err, check.IsNil)

	assertSkopeoSucceeds(c, "", "sync", "--scoped", "--src", "yaml2", "--dest", "docker", "--dest-tls-verify=false", yamlFile, v2DockerRegistryURL)

	localImageRef, err := docker.ParseReference(fmt.Sprintf("//%s/%s", v2DockerRegistryURL, imagePath))
	c.Assert(err, check.IsNil)

	sysCtx := types.SystemContext{
		DockerInsecureSkipTLSVerify: types.NewOptionalBool(true),
	}
	localTags, err := docker.GetRepositoryTags(context.Background(), &sysCtx, localImageRef)
	c.Assert(err, check.IsNil)
	c.Assert(len(localTags), check.Equals, 2)
}

func (s *SyncSuite) TestYamlV2TagRepoRename(c *check.C) {
	image := pullableRepo
	imageRef, err := docker.ParseReference(fmt.Sprintf("//%s", image))
	c.Assert(err, check.IsNil)

	sysCtx := types.SystemContext{}
	tags, err := docker.GetRepositoryTags(context.Background(), &sysCtx, imageRef)
	c.Assert(err, check.IsNil)
	c.Check(len(tags), check.Not(check.Equals), 0)

	localImagePath := "renamed/image"

	yamlConfig := fmt.Sprintf(`
%s:
  repos:
    %s:
      destination-repo: %s
`, reference.Domain(imageRef.DockerReference()), reference.Path(imageRef.DockerReference()), localImagePath)

	tmpDir := c.MkDir()
	yamlFile := path.Join(tmpDir, "registries.yaml")
	err = os.WriteFile(yamlFile, []byte(yamlConfig), 0644)
	c.Assert(err, check.IsNil)
	assertSkopeoSucceeds(c, "", "sync", "--scoped", "--src", "yaml2", "--dest", "docker", "--dest-tls-verify=false", yamlFile, v2DockerRegistryURL)

	localImageRef, err := docker.ParseReference(fmt.Sprintf("//%s/%s", v2DockerRegistryURL, localImagePath))
	c.Assert(err, check.IsNil)

	sysCtx = types.SystemContext{
		DockerInsecureSkipTLSVerify: types.NewOptionalBool(true),
	}
	localTags, err := docker.GetRepositoryTags(context.Background(), &sysCtx, localImageRef)
	c.Assert(err, check.IsNil)
	c.Assert(len(localTags), check.Equals, len(tags))
}
