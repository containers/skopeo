//go:build !windows

package main

/*
  This code is currently only intended to be used by ostree
  to fetch content via containers.  The API is subject
  to change.  A goal however is to stabilize the API
  eventually as a full out-of-process interface to the
  core containers/image library functionality.

  To use this command, in a parent process create a
  `socketpair()` of type `SOCK_SEQPACKET`.  Fork
  off this command, and pass one half of the socket
  pair to the child.  Providing it on stdin (fd 0)
  is the expected default.

  The protocol is JSON for the control layer,
  and  a read side of a `pipe()` passed for large data.

 Base JSON protocol:

 request: { method: "MethodName": args: [arguments] }
 reply: { success: bool, value: JSVAL, pipeid: number, error: string }

 For any non-metadata i.e. payload data from `GetManifest`
 and `GetBlob` the server will pass back the read half of a `pipe(2)` via FD passing,
 along with a `pipeid` integer.

 The expected flow looks like this:

  - Initialize
    And validate the returned protocol version versus
	what your client supports.
  - OpenImage docker://quay.io/someorg/example:latest
    (returns an imageid)
  - GetManifest imageid (and associated <pipeid>)
  (Streaming read data from pipe)
  - FinishPipe <pipeid>
  - GetBlob imageid sha256:...
  (Streaming read data from pipe)
  - FinishPipe <pipeid>
  - GetBlob imageid sha256:...
  (Streaming read data from pipe)
  - FinishPipe <pipeid>
  - CloseImage imageid

 You may interleave invocations of these methods, e.g. one
 can also invoke `OpenImage` multiple times, as well as
 starting multiple GetBlob requests before calling `FinishPipe`
 on them.  The server will stream data into the pipefd
 until `FinishPipe` is invoked.

 Note that the pipe will not be closed by the server until
 the client has invoked `FinishPipe`.  This is to ensure
 that the client checks for errors.  For example, `GetBlob`
 performs digest (e.g. sha256) verification and this must
 be checked after all data has been written.
*/

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"syscall"

	"github.com/containers/image/v5/image"
	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/pkg/blobinfocache"
	"github.com/containers/image/v5/transports"
	"github.com/containers/image/v5/transports/alltransports"
	"github.com/containers/image/v5/types"
	"github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// protocolVersion is semantic version of the protocol used by this proxy.
// The first version of the protocol has major version 0.2 to signify a
// departure from the original code which used HTTP.
//
// 0.2.1: Initial version
// 0.2.2: Added support for fetching image configuration as OCI
// 0.2.3: Added GetFullConfig
// 0.2.4: Added OpenImageOptional
// 0.2.5: Added LayerInfoJSON
// 0.2.6: Policy Verification before pulling OCI
// 0.2.7: Added GetLayerInfoPiped
const protocolVersion = "0.2.7"

// maxMsgSize is the current limit on a packet size.
// Note that all non-metadata (i.e. payload data) is sent over a pipe.
const maxMsgSize = 32 * 1024

// maxJSONFloat is ECMA Number.MAX_SAFE_INTEGER
// https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Number/MAX_SAFE_INTEGER
// We hard error if the input JSON numbers we expect to be
// integers are above this.
const maxJSONFloat = float64(uint64(1)<<53 - 1)

// sentinelImageID represents "image not found" on the wire
const sentinelImageID = 0

// request is the JSON serialization of a function call
type request struct {
	// Method is the name of the function
	Method string `json:"method"`
	// Args is the arguments (parsed inside the function)
	Args []any `json:"args"`
}

// reply is serialized to JSON as the return value from a function call.
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

// replyBuf is our internal deserialization of reply plus optional fd
type replyBuf struct {
	// value will be converted to a reply Value
	value any
	// fd is the read half of a pipe, passed back to the client
	fd *os.File
	// pipeid will be provided to the client as PipeID, an index into our open pipes
	pipeid uint32
}

// activePipe is an open pipe to the client.
// It contains an error value
type activePipe struct {
	// w is the write half of the pipe
	w *os.File
	// wg is completed when our worker goroutine is done
	wg sync.WaitGroup
	// err may be set in our worker goroutine
	err error
}

// openImage is an opened image reference
type openImage struct {
	// id is an opaque integer handle
	id        uint64
	src       types.ImageSource
	cachedimg types.Image
}

// proxyHandler is the state associated with our socket.
type proxyHandler struct {
	// lock protects everything else in this structure.
	lock sync.Mutex
	// opts is CLI options
	opts   *proxyOptions
	sysctx *types.SystemContext
	cache  types.BlobInfoCache

	// imageSerial is a counter for open images
	imageSerial uint64
	// images holds our opened images
	images map[uint64]*openImage
	// activePipes maps from "pipeid" to a pipe + goroutine pair
	activePipes map[uint32]*activePipe
}

// convertedLayerInfo is the reduced form of the OCI type BlobInfo
// Used in the return value of GetLayerInfo
type convertedLayerInfo struct {
	Digest    digest.Digest `json:"digest"`
	Size      int64         `json:"size"`
	MediaType string        `json:"media_type"`
}

// Initialize performs one-time initialization, and returns the protocol version
func (h *proxyHandler) Initialize(args []any) (replyBuf, error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	var ret replyBuf

	if len(args) != 0 {
		return ret, fmt.Errorf("invalid request, expecting zero arguments")
	}

	if h.sysctx != nil {
		return ret, fmt.Errorf("already initialized")
	}

	sysctx, err := h.opts.imageOpts.newSystemContext()
	if err != nil {
		return ret, err
	}
	h.sysctx = sysctx
	h.cache = blobinfocache.DefaultCache(sysctx)

	r := replyBuf{
		value: protocolVersion,
	}
	return r, nil
}

// OpenImage accepts a string image reference i.e. TRANSPORT:REF - like `skopeo copy`.
// The return value is an opaque integer handle.
func (h *proxyHandler) OpenImage(args []any) (replyBuf, error) {
	return h.openImageImpl(args, false)
}

func (h *proxyHandler) openImageImpl(args []any, allowNotFound bool) (retReplyBuf replyBuf, retErr error) {
	h.lock.Lock()
	defer h.lock.Unlock()
	var ret replyBuf

	if h.sysctx == nil {
		return ret, fmt.Errorf("client error: must invoke Initialize")
	}
	if len(args) != 1 {
		return ret, fmt.Errorf("invalid request, expecting one argument")
	}
	imageref, ok := args[0].(string)
	if !ok {
		return ret, fmt.Errorf("expecting string imageref, not %T", args[0])
	}

	imgRef, err := alltransports.ParseImageName(imageref)
	if err != nil {
		return ret, err
	}
	imgsrc, err := imgRef.NewImageSource(context.Background(), h.sysctx)
	if err != nil {
		if allowNotFound && isNotFoundImageError(err) {
			ret.value = sentinelImageID
			return ret, nil
		}
		return ret, err
	}

	policyContext, err := h.opts.global.getPolicyContext()
	if err != nil {
		return ret, err
	}
	defer func() {
		if err := policyContext.Destroy(); err != nil {
			retErr = noteCloseFailure(retErr, "tearing down policy context", err)
		}
	}()

	unparsedTopLevel := image.UnparsedInstance(imgsrc, nil)
	allowed, err := policyContext.IsRunningImageAllowed(context.Background(), unparsedTopLevel)
	if err != nil {
		return ret, err
	}
	if !allowed {
		return ret, fmt.Errorf("internal inconsistency: policy verification failed without returning an error")
	}

	// Note that we never return zero as an imageid; this code doesn't yet
	// handle overflow though.
	h.imageSerial++
	openimg := &openImage{
		id:  h.imageSerial,
		src: imgsrc,
	}
	h.images[openimg.id] = openimg
	ret.value = openimg.id

	return ret, nil
}

// OpenImage accepts a string image reference i.e. TRANSPORT:REF - like `skopeo copy`.
// The return value is an opaque integer handle.  If the image does not exist, zero
// is returned.
func (h *proxyHandler) OpenImageOptional(args []any) (replyBuf, error) {
	return h.openImageImpl(args, true)
}

func (h *proxyHandler) CloseImage(args []any) (replyBuf, error) {
	h.lock.Lock()
	defer h.lock.Unlock()
	var ret replyBuf

	if h.sysctx == nil {
		return ret, fmt.Errorf("client error: must invoke Initialize")
	}
	if len(args) != 1 {
		return ret, fmt.Errorf("invalid request, expecting one argument")
	}
	imgref, err := h.parseImageFromID(args[0])
	if err != nil {
		return ret, err
	}
	imgref.src.Close()
	delete(h.images, imgref.id)

	return ret, nil
}

// parseUint64 validates that a number fits inside a JavaScript safe integer
func parseUint64(v any) (uint64, error) {
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("expecting numeric, not %T", v)
	}
	if f > maxJSONFloat {
		return 0, fmt.Errorf("out of range integer for numeric %f", f)
	}
	return uint64(f), nil
}

func (h *proxyHandler) parseImageFromID(v any) (*openImage, error) {
	imgid, err := parseUint64(v)
	if err != nil {
		return nil, err
	}
	if imgid == sentinelImageID {
		return nil, fmt.Errorf("Invalid imageid value of zero")
	}
	imgref, ok := h.images[imgid]
	if !ok {
		return nil, fmt.Errorf("no image %v", imgid)
	}
	return imgref, nil
}

func (h *proxyHandler) allocPipe() (*os.File, *activePipe, error) {
	piper, pipew, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	f := activePipe{
		w: pipew,
	}
	h.activePipes[uint32(pipew.Fd())] = &f
	f.wg.Add(1)
	return piper, &f, nil
}

// returnBytes generates a return pipe() from a byte array
// In the future it might be nicer to return this via memfd_create()
func (h *proxyHandler) returnBytes(retval any, buf []byte) (replyBuf, error) {
	var ret replyBuf
	piper, f, err := h.allocPipe()
	if err != nil {
		return ret, err
	}

	go func() {
		// Signal completion when we return
		defer f.wg.Done()
		_, err = io.Copy(f.w, bytes.NewReader(buf))
		if err != nil {
			f.err = err
		}
	}()

	ret.value = retval
	ret.fd = piper
	ret.pipeid = uint32(f.w.Fd())
	return ret, nil
}

// cacheTargetManifest is invoked when GetManifest or GetConfig is invoked
// the first time for a given image.  If the requested image is a manifest
// list, this function resolves it to the image matching the calling process'
// operating system and architecture.
//
// TODO: Add GetRawManifest or so that exposes manifest lists
func (h *proxyHandler) cacheTargetManifest(img *openImage) error {
	ctx := context.Background()
	if img.cachedimg != nil {
		return nil
	}
	unparsedToplevel := image.UnparsedInstance(img.src, nil)
	mfest, manifestType, err := unparsedToplevel.Manifest(ctx)
	if err != nil {
		return err
	}
	var target *image.UnparsedImage
	if manifest.MIMETypeIsMultiImage(manifestType) {
		manifestList, err := manifest.ListFromBlob(mfest, manifestType)
		if err != nil {
			return err
		}
		instanceDigest, err := manifestList.ChooseInstance(h.sysctx)
		if err != nil {
			return err
		}
		target = image.UnparsedInstance(img.src, &instanceDigest)
	} else {
		target = unparsedToplevel
	}
	cachedimg, err := image.FromUnparsedImage(ctx, h.sysctx, target)
	if err != nil {
		return err
	}
	img.cachedimg = cachedimg
	return nil
}

// GetManifest returns a copy of the manifest, converted to OCI format, along with the original digest.
// Manifest lists are resolved to the current operating system and architecture.
func (h *proxyHandler) GetManifest(args []any) (replyBuf, error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	var ret replyBuf

	if h.sysctx == nil {
		return ret, fmt.Errorf("client error: must invoke Initialize")
	}
	if len(args) != 1 {
		return ret, fmt.Errorf("invalid request, expecting one argument")
	}
	imgref, err := h.parseImageFromID(args[0])
	if err != nil {
		return ret, err
	}

	err = h.cacheTargetManifest(imgref)
	if err != nil {
		return ret, err
	}
	img := imgref.cachedimg

	ctx := context.Background()
	rawManifest, manifestType, err := img.Manifest(ctx)
	if err != nil {
		return ret, err
	}

	// We only support OCI and docker2schema2.  We know docker2schema2 can be easily+cheaply
	// converted into OCI, so consumers only need to see OCI.
	switch manifestType {
	case imgspecv1.MediaTypeImageManifest, manifest.DockerV2Schema2MediaType:
		break
	// Explicitly reject e.g. docker schema 1 type with a "legacy" note
	case manifest.DockerV2Schema1MediaType, manifest.DockerV2Schema1SignedMediaType:
		return ret, fmt.Errorf("unsupported legacy manifest MIME type: %s", manifestType)
	default:
		return ret, fmt.Errorf("unsupported manifest MIME type: %s", manifestType)
	}

	// We always return the original digest, as that's what clients need to do pull-by-digest
	// and in general identify the image.
	digest, err := manifest.Digest(rawManifest)
	if err != nil {
		return ret, err
	}
	var serialized []byte
	// But, we convert to OCI format on the wire if it's not already.  The idea here is that by reusing the containers/image
	// stack, clients to this proxy can pretend the world is OCI only, and not need to care about e.g.
	// docker schema and MIME types.
	if manifestType != imgspecv1.MediaTypeImageManifest {
		manifestUpdates := types.ManifestUpdateOptions{ManifestMIMEType: imgspecv1.MediaTypeImageManifest}
		ociImage, err := img.UpdatedImage(ctx, manifestUpdates)
		if err != nil {
			return ret, err
		}

		ociSerialized, _, err := ociImage.Manifest(ctx)
		if err != nil {
			return ret, err
		}
		serialized = ociSerialized
	} else {
		serialized = rawManifest
	}
	return h.returnBytes(digest, serialized)
}

// GetFullConfig returns a copy of the image configuration, converted to OCI format.
// https://github.com/opencontainers/image-spec/blob/main/config.md
func (h *proxyHandler) GetFullConfig(args []any) (replyBuf, error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	var ret replyBuf

	if h.sysctx == nil {
		return ret, fmt.Errorf("client error: must invoke Initialize")
	}
	if len(args) != 1 {
		return ret, fmt.Errorf("invalid request, expecting: [imgid]")
	}
	imgref, err := h.parseImageFromID(args[0])
	if err != nil {
		return ret, err
	}
	err = h.cacheTargetManifest(imgref)
	if err != nil {
		return ret, err
	}
	img := imgref.cachedimg

	ctx := context.TODO()
	config, err := img.OCIConfig(ctx)
	if err != nil {
		return ret, err
	}
	serialized, err := json.Marshal(&config)
	if err != nil {
		return ret, err
	}
	return h.returnBytes(nil, serialized)
}

// GetConfig returns a copy of the container runtime configuration, converted to OCI format.
// Note that due to a historical mistake, this returns not the full image configuration,
// but just the container runtime configuration.  You should use GetFullConfig instead.
func (h *proxyHandler) GetConfig(args []any) (replyBuf, error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	var ret replyBuf

	if h.sysctx == nil {
		return ret, fmt.Errorf("client error: must invoke Initialize")
	}
	if len(args) != 1 {
		return ret, fmt.Errorf("invalid request, expecting: [imgid]")
	}
	imgref, err := h.parseImageFromID(args[0])
	if err != nil {
		return ret, err
	}
	err = h.cacheTargetManifest(imgref)
	if err != nil {
		return ret, err
	}
	img := imgref.cachedimg

	ctx := context.TODO()
	config, err := img.OCIConfig(ctx)
	if err != nil {
		return ret, err
	}
	serialized, err := json.Marshal(&config.Config)
	if err != nil {
		return ret, err
	}
	return h.returnBytes(nil, serialized)
}

// GetBlob fetches a blob, performing digest verification.
func (h *proxyHandler) GetBlob(args []any) (replyBuf, error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	var ret replyBuf

	if h.sysctx == nil {
		return ret, fmt.Errorf("client error: must invoke Initialize")
	}
	if len(args) != 3 {
		return ret, fmt.Errorf("found %d args, expecting (imgid, digest, size)", len(args))
	}
	imgref, err := h.parseImageFromID(args[0])
	if err != nil {
		return ret, err
	}
	digestStr, ok := args[1].(string)
	if !ok {
		return ret, fmt.Errorf("expecting string blobid")
	}
	size, err := parseUint64(args[2])
	if err != nil {
		return ret, err
	}

	ctx := context.TODO()
	d, err := digest.Parse(digestStr)
	if err != nil {
		return ret, err
	}
	blobr, blobSize, err := imgref.src.GetBlob(ctx, types.BlobInfo{Digest: d, Size: int64(size)}, h.cache)
	if err != nil {
		return ret, err
	}

	piper, f, err := h.allocPipe()
	if err != nil {
		blobr.Close()
		return ret, err
	}
	go func() {
		// Signal completion when we return
		defer blobr.Close()
		defer f.wg.Done()
		verifier := d.Verifier()
		tr := io.TeeReader(blobr, verifier)
		n, err := io.Copy(f.w, tr)
		if err != nil {
			f.err = err
			return
		}
		if n != int64(size) {
			f.err = fmt.Errorf("expected %d bytes in blob, got %d", size, n)
		}
		if !verifier.Verified() {
			f.err = fmt.Errorf("corrupted blob, expecting %s", d.String())
		}
	}()

	ret.value = blobSize
	ret.fd = piper
	ret.pipeid = uint32(f.w.Fd())
	return ret, nil
}

// GetLayerInfo returns data about the layers of an image, useful for reading the layer contents.
//
// This is the same as GetLayerInfoPiped, but returns its contents inline. This is subject to
// failure for large images (because we use SOCK_SEQPACKET which has a maximum buffer size)
// and is hence only retained for backwards compatibility. Callers are expected to use
// the semver to know whether they can call the new API.
func (h *proxyHandler) GetLayerInfo(args []any) (replyBuf, error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	var ret replyBuf

	if h.sysctx == nil {
		return ret, fmt.Errorf("client error: must invoke Initialize")
	}

	if len(args) != 1 {
		return ret, fmt.Errorf("found %d args, expecting (imgid)", len(args))
	}

	imgref, err := h.parseImageFromID(args[0])
	if err != nil {
		return ret, err
	}

	ctx := context.TODO()

	err = h.cacheTargetManifest(imgref)
	if err != nil {
		return ret, err
	}
	img := imgref.cachedimg

	layerInfos, err := img.LayerInfosForCopy(ctx)
	if err != nil {
		return ret, err
	}

	if layerInfos == nil {
		layerInfos = img.LayerInfos()
	}

	layers := make([]convertedLayerInfo, 0, len(layerInfos))
	for _, layer := range layerInfos {
		layers = append(layers, convertedLayerInfo{layer.Digest, layer.Size, layer.MediaType})
	}

	ret.value = layers
	return ret, nil
}

// GetLayerInfoPiped returns data about the layers of an image, useful for reading the layer contents.
//
// This needs to be called since the data returned by GetManifest() does not allow to correctly
// calling GetBlob() for the containers-storage: transport (which doesn’t store the original compressed
// representations referenced in the manifest).
func (h *proxyHandler) GetLayerInfoPiped(args []any) (replyBuf, error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	var ret replyBuf

	if h.sysctx == nil {
		return ret, fmt.Errorf("client error: must invoke Initialize")
	}

	if len(args) != 1 {
		return ret, fmt.Errorf("found %d args, expecting (imgid)", len(args))
	}

	imgref, err := h.parseImageFromID(args[0])
	if err != nil {
		return ret, err
	}

	ctx := context.TODO()

	err = h.cacheTargetManifest(imgref)
	if err != nil {
		return ret, err
	}
	img := imgref.cachedimg

	layerInfos, err := img.LayerInfosForCopy(ctx)
	if err != nil {
		return ret, err
	}

	if layerInfos == nil {
		layerInfos = img.LayerInfos()
	}

	layers := make([]convertedLayerInfo, 0, len(layerInfos))
	for _, layer := range layerInfos {
		layers = append(layers, convertedLayerInfo{layer.Digest, layer.Size, layer.MediaType})
	}

	serialized, err := json.Marshal(&layers)
	if err != nil {
		return ret, err
	}
	return h.returnBytes(nil, serialized)
}

// FinishPipe waits for the worker goroutine to finish, and closes the write side of the pipe.
func (h *proxyHandler) FinishPipe(args []any) (replyBuf, error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	var ret replyBuf

	pipeidv, err := parseUint64(args[0])
	if err != nil {
		return ret, err
	}
	pipeid := uint32(pipeidv)

	f, ok := h.activePipes[pipeid]
	if !ok {
		return ret, fmt.Errorf("finishpipe: no active pipe %d", pipeid)
	}

	// Wait for the goroutine to complete
	f.wg.Wait()
	logrus.Debug("Completed pipe goroutine")
	// And only now do we close the write half; this forces the client to call this API
	f.w.Close()
	// Propagate any errors from the goroutine worker
	err = f.err
	delete(h.activePipes, pipeid)
	return ret, err
}

// close releases all resources associated with this proxy backend
func (h *proxyHandler) close() {
	for _, image := range h.images {
		err := image.src.Close()
		if err != nil {
			// This shouldn't be fatal
			logrus.Warnf("Failed to close image %s: %v", transports.ImageName(image.cachedimg.Reference()), err)
		}
	}
}

// send writes a reply buffer to the socket
func (buf replyBuf) send(conn *net.UnixConn, err error) error {
	logrus.Debugf("Sending reply: err=%v value=%v pipeid=%v", err, buf.value, buf.pipeid)
	replyToSerialize := reply{
		Success: err == nil,
		Value:   buf.value,
		PipeID:  buf.pipeid,
	}
	if err != nil {
		replyToSerialize.Error = err.Error()
	}
	serializedReply, err := json.Marshal(&replyToSerialize)
	if err != nil {
		return err
	}
	// We took ownership of the FD - close it when we're done.
	defer func() {
		if buf.fd != nil {
			buf.fd.Close()
		}
	}()
	// Copy the FD number to the socket ancillary buffer
	fds := make([]int, 0)
	if buf.fd != nil {
		fds = append(fds, int(buf.fd.Fd()))
	}
	oob := syscall.UnixRights(fds...)
	n, oobn, err := conn.WriteMsgUnix(serializedReply, oob, nil)
	if err != nil {
		return err
	}
	// Validate that we sent the full packet
	if n != len(serializedReply) || oobn != len(oob) {
		return io.ErrShortWrite
	}
	return nil
}

type proxyOptions struct {
	global    *globalOptions
	imageOpts *imageOptions
	sockFd    int
}

func proxyCmd(global *globalOptions) *cobra.Command {
	sharedFlags, sharedOpts := sharedImageFlags()
	imageFlags, imageOpts := imageFlags(global, sharedOpts, nil, "", "")
	opts := proxyOptions{global: global, imageOpts: imageOpts}
	cmd := &cobra.Command{
		Use:   "experimental-image-proxy [command options] IMAGE",
		Short: "Interactive proxy for fetching container images (EXPERIMENTAL)",
		Long:  `Run skopeo as a proxy, supporting HTTP requests to fetch manifests and blobs.`,
		RunE:  commandAction(opts.run),
		Args:  cobra.ExactArgs(0),
		// Not stabilized yet
		Hidden:  true,
		Example: `skopeo experimental-image-proxy --sockfd 3`,
	}
	adjustUsage(cmd)
	flags := cmd.Flags()
	flags.AddFlagSet(&sharedFlags)
	flags.AddFlagSet(&imageFlags)
	flags.IntVar(&opts.sockFd, "sockfd", 0, "Serve on opened socket pair (default 0/stdin)")
	return cmd
}

// processRequest dispatches a remote request.
// replyBuf is the result of the invocation.
// terminate should be true if processing of requests should halt.
func (h *proxyHandler) processRequest(readBytes []byte) (rb replyBuf, terminate bool, err error) {
	var req request

	// Parse the request JSON
	if err = json.Unmarshal(readBytes, &req); err != nil {
		err = fmt.Errorf("invalid request: %v", err)
		return
	}
	logrus.Debugf("Executing method %s", req.Method)

	// Dispatch on the method
	switch req.Method {
	case "Initialize":
		rb, err = h.Initialize(req.Args)
	case "OpenImage":
		rb, err = h.OpenImage(req.Args)
	case "OpenImageOptional":
		rb, err = h.OpenImageOptional(req.Args)
	case "CloseImage":
		rb, err = h.CloseImage(req.Args)
	case "GetManifest":
		rb, err = h.GetManifest(req.Args)
	case "GetConfig":
		rb, err = h.GetConfig(req.Args)
	case "GetFullConfig":
		rb, err = h.GetFullConfig(req.Args)
	case "GetBlob":
		rb, err = h.GetBlob(req.Args)
	case "GetLayerInfo":
		rb, err = h.GetLayerInfo(req.Args)
	case "GetLayerInfoPiped":
		rb, err = h.GetLayerInfoPiped(req.Args)
	case "FinishPipe":
		rb, err = h.FinishPipe(req.Args)
	case "Shutdown":
		terminate = true
	// NOTE: If you add a method here, you should very likely be bumping the
	// const protocolVersion above.
	default:
		err = fmt.Errorf("unknown method: %s", req.Method)
	}
	return
}

// Implementation of podman experimental-image-proxy
func (opts *proxyOptions) run(args []string, stdout io.Writer) error {
	handler := &proxyHandler{
		opts:        opts,
		images:      make(map[uint64]*openImage),
		activePipes: make(map[uint32]*activePipe),
	}
	defer handler.close()

	// Convert the socket FD passed by client into a net.FileConn
	fd := os.NewFile(uintptr(opts.sockFd), "sock")
	fconn, err := net.FileConn(fd)
	if err != nil {
		return err
	}
	conn := fconn.(*net.UnixConn)

	// Allocate a buffer to copy the packet into
	buf := make([]byte, maxMsgSize)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("reading socket: %v", err)
		}
		readbuf := buf[0:n]

		rb, terminate, err := handler.processRequest(readbuf)
		if terminate {
			logrus.Debug("terminating")
			return nil
		}

		if err := rb.send(conn, err); err != nil {
			return fmt.Errorf("writing to socket: %w", err)
		}
	}
}
