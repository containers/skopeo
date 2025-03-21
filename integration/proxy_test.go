package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/containers/image/v5/manifest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// This image is known to be x86_64 only right now
const knownNotManifestListedImageX8664 = "docker://quay.io/coreos/11bot"

// knownNotExtantImage would be very surprising if it did exist
const knownNotExtantImage = "docker://quay.io/centos/centos:opensusewindowsubuntu"

const expectedProxySemverMajor = "0.2"

// request is copied from proxy.go
// We intentionally copy to ensure that we catch any unexpected "API" changes
// in the JSON.
type request struct {
	// Method is the name of the function
	Method string `json:"method"`
	// Args is the arguments (parsed inside the function)
	Args []any `json:"args"`
}

// reply is copied from proxy.go
type reply struct {
	// Success is true if and only if the call succeeded.
	Success bool `json:"success"`
	// Value is an arbitrary value (or values, as array/map) returned from the call.
	Value any `json:"value"`
	// PipeID is an index into open pipes, and should be passed to FinishPipe
	PipeID uint32 `json:"pipeid"`
	// Error should be non-empty if Success == false
	Error string `json:"error"`
}

// maxMsgSize is also copied from proxy.go
const maxMsgSize = 32 * 1024

type proxy struct {
	c *net.UnixConn
}

type pipefd struct {
	// id is the remote identifier "pipeid"
	id uint
	fd *os.File
}

func (p *proxy) call(method string, args []any) (rval any, fd *pipefd, err error) {
	req := request{
		Method: method,
		Args:   args,
	}
	reqbuf, err := json.Marshal(&req)
	if err != nil {
		return
	}
	n, err := p.c.Write(reqbuf)
	if err != nil {
		return
	}
	if n != len(reqbuf) {
		err = fmt.Errorf("short write during call of %d bytes", n)
		return
	}
	oob := make([]byte, syscall.CmsgSpace(1))
	replybuf := make([]byte, maxMsgSize)
	n, oobn, _, _, err := p.c.ReadMsgUnix(replybuf, oob)
	if err != nil {
		err = fmt.Errorf("reading reply: %w", err)
		return
	}
	var reply reply
	err = json.Unmarshal(replybuf[0:n], &reply)
	if err != nil {
		err = fmt.Errorf("Failed to parse reply: %w", err)
		return
	}
	if !reply.Success {
		err = fmt.Errorf("remote error: %s", reply.Error)
		return
	}

	if reply.PipeID > 0 {
		var scms []syscall.SocketControlMessage
		scms, err = syscall.ParseSocketControlMessage(oob[:oobn])
		if err != nil {
			err = fmt.Errorf("failed to parse control message: %w", err)
			return
		}
		if len(scms) != 1 {
			err = fmt.Errorf("Expected 1 received fd, found %d", len(scms))
			return
		}
		var fds []int
		fds, err = syscall.ParseUnixRights(&scms[0])
		if err != nil {
			err = fmt.Errorf("failed to parse unix rights: %w", err)
			return
		}
		fd = &pipefd{
			fd: os.NewFile(uintptr(fds[0]), "replyfd"),
			id: uint(reply.PipeID),
		}
	}

	rval = reply.Value
	return
}

func (p *proxy) callNoFd(method string, args []any) (rval any, err error) {
	var fd *pipefd
	rval, fd, err = p.call(method, args)
	if err != nil {
		return
	}
	if fd != nil {
		err = fmt.Errorf("Unexpected fd from method %s", method)
		return
	}
	return rval, nil
}

func (p *proxy) callReadAllBytes(method string, args []any) (rval any, buf []byte, err error) {
	var fd *pipefd
	rval, fd, err = p.call(method, args)
	if err != nil {
		return
	}
	if fd == nil {
		err = fmt.Errorf("Expected fd from method %s", method)
		return
	}
	fetchchan := make(chan byteFetch)
	go func() {
		manifestBytes, err := io.ReadAll(fd.fd)
		fetchchan <- byteFetch{
			content: manifestBytes,
			err:     err,
		}
	}()
	_, err = p.callNoFd("FinishPipe", []any{fd.id})
	if err != nil {
		return
	}
	select {
	case fetchRes := <-fetchchan:
		err = fetchRes.err
		if err != nil {
			return
		}

		buf = fetchRes.content
	case <-time.After(5 * time.Minute):
		err = fmt.Errorf("timed out during proxy fetch")
	}
	return
}

func newProxy() (*proxy, error) {
	fds, err := syscall.Socketpair(syscall.AF_LOCAL, syscall.SOCK_SEQPACKET, 0)
	if err != nil {
		return nil, err
	}
	myfd := os.NewFile(uintptr(fds[0]), "myfd")
	defer myfd.Close()
	theirfd := os.NewFile(uintptr(fds[1]), "theirfd")
	defer theirfd.Close()

	mysock, err := net.FileConn(myfd)
	if err != nil {
		return nil, err
	}

	// Note ExtraFiles starts at 3
	proc := exec.Command("skopeo", "experimental-image-proxy", "--sockfd", "3")
	proc.Stderr = os.Stderr
	cmdLifecycleToParentIfPossible(proc)
	proc.ExtraFiles = append(proc.ExtraFiles, theirfd)

	if err = proc.Start(); err != nil {
		return nil, err
	}

	p := &proxy{
		c: mysock.(*net.UnixConn),
	}

	v, err := p.callNoFd("Initialize", nil)
	if err != nil {
		return nil, err
	}
	semver, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("proxy Initialize: Unexpected value %T", v)
	}
	if !strings.HasPrefix(semver, expectedProxySemverMajor) {
		return nil, fmt.Errorf("Unexpected semver %s", semver)
	}
	return p, nil
}

func TestProxy(t *testing.T) {
	suite.Run(t, &proxySuite{})
}

type proxySuite struct {
	suite.Suite
}

type byteFetch struct {
	content []byte
	err     error
}

// This exercises all the metadata fetching APIs.
func runTestMetadataAPIs(p *proxy, img string) error {
	v, err := p.callNoFd("OpenImage", []any{img})
	if err != nil {
		return err
	}

	imgidv, ok := v.(float64)
	if !ok {
		return fmt.Errorf("OpenImage return value is %T", v)
	}
	imgid := uint64(imgidv)
	if imgid == 0 {
		return fmt.Errorf("got zero from expected image")
	}

	// Also verify the optional path
	v, err = p.callNoFd("OpenImageOptional", []any{img})
	if err != nil {
		return err
	}

	imgidv, ok = v.(float64)
	if !ok {
		return fmt.Errorf("OpenImageOptional return value is %T", v)
	}
	imgid2 := uint64(imgidv)
	if imgid2 == 0 {
		return fmt.Errorf("got zero from expected image")
	}

	_, err = p.callNoFd("CloseImage", []any{imgid2})
	if err != nil {
		return err
	}

	_, manifestBytes, err := p.callReadAllBytes("GetManifest", []any{imgid})
	if err != nil {
		return err
	}
	_, err = manifest.OCI1FromManifest(manifestBytes)
	if err != nil {
		return err
	}

	_, configBytes, err := p.callReadAllBytes("GetFullConfig", []any{imgid})
	if err != nil {
		return err
	}
	var config imgspecv1.Image
	err = json.Unmarshal(configBytes, &config)
	if err != nil {
		return err
	}

	// Validate that the image config seems sane
	if config.Architecture == "" {
		return fmt.Errorf("No architecture found")
	}
	if len(config.Config.Cmd) == 0 && len(config.Config.Entrypoint) == 0 {
		return fmt.Errorf("No CMD or ENTRYPOINT set")
	}

	_, layerInfoBytes, err := p.callReadAllBytes("GetLayerInfoPiped", []any{imgid})
	if err != nil {
		return err
	}
	var layerInfoBytesData []interface{}
	err = json.Unmarshal(layerInfoBytes, &layerInfoBytesData)
	if err != nil {
		return err
	}
	if len(layerInfoBytesData) == 0 {
		return fmt.Errorf("expected layer info data")
	}

	// Also test this legacy interface
	_, ctrconfigBytes, err := p.callReadAllBytes("GetConfig", []any{imgid})
	if err != nil {
		return err
	}
	var ctrconfig imgspecv1.ImageConfig
	err = json.Unmarshal(ctrconfigBytes, &ctrconfig)
	if err != nil {
		return err
	}

	// Validate that the config seems sane
	if len(ctrconfig.Cmd) == 0 && len(ctrconfig.Entrypoint) == 0 {
		return fmt.Errorf("No CMD or ENTRYPOINT set")
	}

	_, err = p.callNoFd("CloseImage", []any{imgid})
	if err != nil {
		return err
	}

	return nil
}

func runTestOpenImageOptionalNotFound(p *proxy, img string) error {
	v, err := p.callNoFd("OpenImageOptional", []any{img})
	if err != nil {
		return err
	}

	imgidv, ok := v.(float64)
	if !ok {
		return fmt.Errorf("OpenImageOptional return value is %T", v)
	}
	imgid := uint64(imgidv)
	if imgid != 0 {
		return fmt.Errorf("Unexpected optional image id %v", imgid)
	}
	return nil
}

func (s *proxySuite) TestProxy() {
	t := s.T()
	p, err := newProxy()
	require.NoError(t, err)

	err = runTestMetadataAPIs(p, knownNotManifestListedImageX8664)
	if err != nil {
		err = fmt.Errorf("Testing image %s: %v", knownNotManifestListedImageX8664, err)
	}
	assert.NoError(t, err)

	err = runTestMetadataAPIs(p, knownListImage)
	if err != nil {
		err = fmt.Errorf("Testing image %s: %v", knownListImage, err)
	}
	assert.NoError(t, err)

	err = runTestOpenImageOptionalNotFound(p, knownNotExtantImage)
	if err != nil {
		err = fmt.Errorf("Testing optional image %s: %v", knownNotExtantImage, err)
	}
	assert.NoError(t, err)
}
