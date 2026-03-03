package jsonproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"

	"github.com/opencontainers/go-digest"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"go.podman.io/image/v5/image"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/pkg/blobinfocache"
	"go.podman.io/image/v5/signature"
	"go.podman.io/image/v5/transports/alltransports"
	"go.podman.io/image/v5/types"
)

// handler is the core request processing logic.
type handler struct {
	// lock protects everything else in this structure.
	lock sync.Mutex

	// Dependency injection functions.
	getSystemContext func() (*types.SystemContext, error)
	getPolicyContext func() (*signature.PolicyContext, error)
	logger           logrus.FieldLogger

	// Internal state.
	sysctx      *types.SystemContext
	cache       types.BlobInfoCache
	imageSerial uint64
	images      map[uint64]*openImage
	activePipes map[uint32]*activePipe
}

// newHandler creates a new handler with the given dependencies.
func newHandler(getSystemContext func() (*types.SystemContext, error), getPolicyContext func() (*signature.PolicyContext, error), logger logrus.FieldLogger) *handler {
	return &handler{
		getSystemContext: getSystemContext,
		getPolicyContext: getPolicyContext,
		logger:           logger,
		images:           make(map[uint64]*openImage),
		activePipes:      make(map[uint32]*activePipe),
	}
}

// close releases all resources associated with this handler.
func (h *handler) close() {
	h.lock.Lock()
	defer h.lock.Unlock()

	for _, image := range h.images {
		if err := image.src.Close(); err != nil {
			// This shouldn't be fatal
			h.logger.Warnf("Failed to close image: %v", err)
		}
	}
}

// Initialize performs one-time initialization, and returns the protocol version.
func (h *handler) Initialize(ctx context.Context, args []any) (replyBuf, error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	var ret replyBuf

	if len(args) != 0 {
		return ret, errors.New("invalid request, expecting zero arguments")
	}

	if h.sysctx != nil {
		return ret, errors.New("already initialized")
	}

	sysctx, err := h.getSystemContext()
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
func (h *handler) OpenImage(ctx context.Context, args []any) (replyBuf, error) {
	return h.openImageImpl(ctx, args, false)
}

func (h *handler) openImageImpl(ctx context.Context, args []any, allowNotFound bool) (retReplyBuf replyBuf, retErr error) {
	h.lock.Lock()
	defer h.lock.Unlock()
	var ret replyBuf

	if h.sysctx == nil {
		return ret, errors.New("client error: must invoke Initialize")
	}
	if len(args) != 1 {
		return ret, errors.New("invalid request, expecting one argument")
	}
	imageref, ok := args[0].(string)
	if !ok {
		return ret, fmt.Errorf("expecting string imageref, not %T", args[0])
	}

	imgRef, err := alltransports.ParseImageName(imageref)
	if err != nil {
		return ret, err
	}
	imgsrc, err := imgRef.NewImageSource(ctx, h.sysctx)
	if err != nil {
		if allowNotFound && isNotFoundImageError(err) {
			ret.value = sentinelImageID
			return ret, nil
		}
		return ret, err
	}

	policyContext, err := h.getPolicyContext()
	if err != nil {
		return ret, err
	}
	defer func() {
		if err := policyContext.Destroy(); err != nil {
			retErr = noteCloseFailure(retErr, "tearing down policy context", err)
		}
	}()

	unparsedTopLevel := image.UnparsedInstance(imgsrc, nil)
	allowed, err := policyContext.IsRunningImageAllowed(ctx, unparsedTopLevel)
	if err != nil {
		return ret, err
	}
	if !allowed {
		return ret, errors.New("internal inconsistency: policy verification failed without returning an error")
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

// OpenImageOptional accepts a string image reference i.e. TRANSPORT:REF - like `skopeo copy`.
// The return value is an opaque integer handle.  If the image does not exist, zero
// is returned.
func (h *handler) OpenImageOptional(ctx context.Context, args []any) (replyBuf, error) {
	return h.openImageImpl(ctx, args, true)
}

func (h *handler) CloseImage(ctx context.Context, args []any) (replyBuf, error) {
	h.lock.Lock()
	defer h.lock.Unlock()
	var ret replyBuf

	if h.sysctx == nil {
		return ret, errors.New("client error: must invoke Initialize")
	}
	if len(args) != 1 {
		return ret, errors.New("invalid request, expecting one argument")
	}
	imgref, err := h.parseImageFromID(args[0])
	if err != nil {
		return ret, err
	}
	imgref.src.Close()
	delete(h.images, imgref.id)

	return ret, nil
}

// parseUint64 validates that a number fits inside a JavaScript safe integer.
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

func (h *handler) parseImageFromID(v any) (*openImage, error) {
	imgid, err := parseUint64(v)
	if err != nil {
		return nil, err
	}
	if imgid == sentinelImageID {
		return nil, errors.New("invalid imageid value of zero")
	}
	imgref, ok := h.images[imgid]
	if !ok {
		return nil, fmt.Errorf("no image %v", imgid)
	}
	return imgref, nil
}

func (h *handler) allocPipe() (*os.File, *activePipe, error) {
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

// returnBytes generates a return pipe() from a byte array.
// In the future it might be nicer to return this via memfd_create().
func (h *handler) returnBytes(retval any, buf []byte) (replyBuf, error) {
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
// TODO: Add GetRawManifest or so that exposes manifest lists.
func (h *handler) cacheTargetManifest(ctx context.Context, img *openImage) error {
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
func (h *handler) GetManifest(ctx context.Context, args []any) (replyBuf, error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	var ret replyBuf

	if h.sysctx == nil {
		return ret, errors.New("client error: must invoke Initialize")
	}
	if len(args) != 1 {
		return ret, errors.New("invalid request, expecting one argument")
	}
	imgref, err := h.parseImageFromID(args[0])
	if err != nil {
		return ret, err
	}

	err = h.cacheTargetManifest(ctx, imgref)
	if err != nil {
		return ret, err
	}
	img := imgref.cachedimg

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
func (h *handler) GetFullConfig(ctx context.Context, args []any) (replyBuf, error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	var ret replyBuf

	if h.sysctx == nil {
		return ret, errors.New("client error: must invoke Initialize")
	}
	if len(args) != 1 {
		return ret, errors.New("invalid request, expecting: [imgid]")
	}
	imgref, err := h.parseImageFromID(args[0])
	if err != nil {
		return ret, err
	}
	err = h.cacheTargetManifest(ctx, imgref)
	if err != nil {
		return ret, err
	}
	img := imgref.cachedimg

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
func (h *handler) GetConfig(ctx context.Context, args []any) (replyBuf, error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	var ret replyBuf

	if h.sysctx == nil {
		return ret, errors.New("client error: must invoke Initialize")
	}
	if len(args) != 1 {
		return ret, errors.New("invalid request, expecting: [imgid]")
	}
	imgref, err := h.parseImageFromID(args[0])
	if err != nil {
		return ret, err
	}
	err = h.cacheTargetManifest(ctx, imgref)
	if err != nil {
		return ret, err
	}
	img := imgref.cachedimg

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
func (h *handler) GetBlob(ctx context.Context, args []any) (replyBuf, error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	var ret replyBuf

	if h.sysctx == nil {
		return ret, errors.New("client error: must invoke Initialize")
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
		return ret, errors.New("expecting string blobid")
	}
	size, err := parseUint64(args[2])
	if err != nil {
		return ret, err
	}

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

// GetRawBlob can be viewed as a more general purpose successor
// to GetBlob. First, it does not verify the digest, which in
// some cases is unnecessary as the client would prefer to do it.
//
// It also does not use the "FinishPipe" API call, but instead
// returns *two* file descriptors, one for errors and one for data.
//
// On (initial) success, the return value provided to the client is the size of the blob.
func (h *handler) GetRawBlob(ctx context.Context, args []any) (replyBuf, error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	var ret replyBuf

	if h.sysctx == nil {
		return ret, errors.New("client error: must invoke Initialize")
	}
	if len(args) != 2 {
		return ret, fmt.Errorf("found %d args, expecting (imgid, digest)", len(args))
	}
	imgref, err := h.parseImageFromID(args[0])
	if err != nil {
		return ret, err
	}
	digestStr, ok := args[1].(string)
	if !ok {
		return ret, errors.New("expecting string blobid")
	}

	d, err := digest.Parse(digestStr)
	if err != nil {
		return ret, err
	}
	blobr, blobSize, err := imgref.src.GetBlob(ctx, types.BlobInfo{Digest: d, Size: int64(-1)}, h.cache)
	if err != nil {
		return ret, err
	}

	// Note this doesn't call allocPipe; we're not using the FinishPipe infrastructure.
	piper, pipew, err := os.Pipe()
	if err != nil {
		blobr.Close()
		return ret, err
	}
	errpipeR, errpipeW, err := os.Pipe()
	if err != nil {
		piper.Close()
		pipew.Close()
		blobr.Close()
		return ret, err
	}
	// Asynchronous worker doing a copy
	go func() {
		// We own the read from registry, and write pipe objects
		defer blobr.Close()
		defer pipew.Close()
		defer errpipeW.Close()
		h.logger.Debugf("Copying blob to client: %d bytes", blobSize)
		_, err := io.Copy(pipew, blobr)
		// Handle errors here by serializing a JSON error back over
		// the error channel. In either case, both file descriptors
		// will be closed, signaling the completion of the operation.
		if err != nil {
			h.logger.Debugf("Sending error to client: %v", err)
			serializedErr := newProxyError(err)
			buf, marshalErr := json.Marshal(serializedErr)
			if marshalErr != nil {
				h.logger.Errorf("Failed to marshal error: %v", marshalErr)
				return
			}
			_, writeErr := errpipeW.Write(buf)
			if writeErr != nil && !errors.Is(writeErr, syscall.EPIPE) {
				h.logger.Debugf("Writing to client: %v", writeErr)
			}
		}
		h.logger.Debugf("Completed GetRawBlob operation")
	}()

	ret.value = blobSize
	ret.fd = piper
	ret.errfd = errpipeR
	return ret, nil
}

// GetLayerInfo returns data about the layers of an image, useful for reading the layer contents.
//
// This is the same as GetLayerInfoPiped, but returns its contents inline. This is subject to
// failure for large images (because we use SOCK_SEQPACKET which has a maximum buffer size)
// and is hence only retained for backwards compatibility. Callers are expected to use
// the semver to know whether they can call the new API.
func (h *handler) GetLayerInfo(ctx context.Context, args []any) (replyBuf, error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	var ret replyBuf

	if h.sysctx == nil {
		return ret, errors.New("client error: must invoke Initialize")
	}

	if len(args) != 1 {
		return ret, fmt.Errorf("found %d args, expecting (imgid)", len(args))
	}

	imgref, err := h.parseImageFromID(args[0])
	if err != nil {
		return ret, err
	}

	err = h.cacheTargetManifest(ctx, imgref)
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
// calling GetBlob() for the containers-storage: transport (which doesn't store the original compressed
// representations referenced in the manifest).
func (h *handler) GetLayerInfoPiped(ctx context.Context, args []any) (replyBuf, error) {
	h.lock.Lock()
	defer h.lock.Unlock()

	var ret replyBuf

	if h.sysctx == nil {
		return ret, errors.New("client error: must invoke Initialize")
	}

	if len(args) != 1 {
		return ret, fmt.Errorf("found %d args, expecting (imgid)", len(args))
	}

	imgref, err := h.parseImageFromID(args[0])
	if err != nil {
		return ret, err
	}

	err = h.cacheTargetManifest(ctx, imgref)
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
func (h *handler) FinishPipe(ctx context.Context, args []any) (replyBuf, error) {
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
	h.logger.Debug("Completed pipe goroutine")
	// And only now do we close the write half; this forces the client to call this API
	f.w.Close()
	// Propagate any errors from the goroutine worker
	err = f.err
	delete(h.activePipes, pipeid)
	return ret, err
}

// processRequest dispatches a remote request.
// replyBuf is the result of the invocation.
// terminate should be true if processing of requests should halt.
func (h *handler) processRequest(ctx context.Context, readBytes []byte) (rb replyBuf, terminate bool, err error) {
	var req request

	// Parse the request JSON
	if err = json.Unmarshal(readBytes, &req); err != nil {
		err = fmt.Errorf("invalid request: %v", err)
		return
	}
	h.logger.Debugf("Executing method %s", req.Method)

	// Dispatch on the method
	switch req.Method {
	case "Initialize":
		rb, err = h.Initialize(ctx, req.Args)
	case "OpenImage":
		rb, err = h.OpenImage(ctx, req.Args)
	case "OpenImageOptional":
		rb, err = h.OpenImageOptional(ctx, req.Args)
	case "CloseImage":
		rb, err = h.CloseImage(ctx, req.Args)
	case "GetManifest":
		rb, err = h.GetManifest(ctx, req.Args)
	case "GetConfig":
		rb, err = h.GetConfig(ctx, req.Args)
	case "GetFullConfig":
		rb, err = h.GetFullConfig(ctx, req.Args)
	case "GetBlob":
		rb, err = h.GetBlob(ctx, req.Args)
	case "GetRawBlob":
		rb, err = h.GetRawBlob(ctx, req.Args)
	case "GetLayerInfo":
		rb, err = h.GetLayerInfo(ctx, req.Args)
	case "GetLayerInfoPiped":
		rb, err = h.GetLayerInfoPiped(ctx, req.Args)
	case "FinishPipe":
		rb, err = h.FinishPipe(ctx, req.Args)
	case "Shutdown":
		terminate = true
	// NOTE: If you add a method here, you should very likely be bumping the
	// const protocolVersion above.
	default:
		err = fmt.Errorf("unknown method: %s", req.Method)
	}
	return
}
