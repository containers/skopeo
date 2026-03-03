package jsonproxy

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"

	"github.com/opencontainers/go-digest"
	"go.podman.io/common/pkg/retry"
	"go.podman.io/image/v5/types"
)

// protocolVersion is semantic version of the protocol used by this proxy.
// The first version of the protocol has major version 0.2 to signify a
// departure from the original code which used HTTP.
//
// When bumping this, please also update the man page.
const protocolVersion = "0.2.8"

// maxMsgSize is the current limit on a packet size.
// Note that all non-metadata (i.e. payload data) is sent over a pipe.
const maxMsgSize = 32 * 1024

// maxJSONFloat is ECMA Number.MAX_SAFE_INTEGER
// https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Number/MAX_SAFE_INTEGER
// We hard error if the input JSON numbers we expect to be
// integers are above this.
const maxJSONFloat = float64(uint64(1)<<53 - 1)

// sentinelImageID represents "image not found" on the wire.
const sentinelImageID = 0

// request is the JSON serialization of a function call.
type request struct {
	// Method is the name of the function.
	Method string `json:"method"`
	// Args is the arguments (parsed inside the function).
	Args []any `json:"args"`
}

type proxyErrorCode string

const (
	// proxyErrPipe means we got EPIPE writing to a pipe owned by the client.
	proxyErrPipe proxyErrorCode = "EPIPE"
	// proxyErrRetryable can be used by clients to automatically retry operations.
	proxyErrRetryable proxyErrorCode = "retryable"
	// proxyErrOther represents all other errors.
	proxyErrOther proxyErrorCode = "other"
)

// proxyError is serialized over the errfd channel for GetRawBlob.
type proxyError struct {
	Code    proxyErrorCode `json:"code"`
	Message string         `json:"message"`
}

// reply is serialized to JSON as the return value from a function call.
type reply struct {
	// Success is true if and only if the call succeeded.
	Success bool `json:"success"`
	// Value is an arbitrary value (or values, as array/map) returned from the call.
	Value any `json:"value"`
	// PipeID is an index into open pipes, and should be passed to FinishPipe.
	PipeID uint32 `json:"pipeid"`
	// ErrorCode will be non-empty if error is set (new in 0.2.8).
	ErrorCode proxyErrorCode `json:"error_code"`
	// Error should be non-empty if Success == false.
	Error string `json:"error"`
}

// replyBuf is our internal deserialization of reply plus optional fd.
type replyBuf struct {
	// value will be converted to a reply Value.
	value any
	// fd is the read half of a pipe, passed back to the client for additional data.
	fd *os.File
	// errfd will be a serialization of error state. This is optional and is currently
	// only used by GetRawBlob.
	errfd *os.File
	// pipeid will be provided to the client as PipeID, an index into our open pipes.
	pipeid uint32
}

// activePipe is an open pipe to the client
// that contains an error value.
type activePipe struct {
	// w is the write half of the pipe.
	w *os.File
	// wg is completed when our worker goroutine is done.
	wg sync.WaitGroup
	// err may be set in our worker goroutine.
	err error
}

// openImage is an opened image reference.
type openImage struct {
	// id is an opaque integer handle.
	id        uint64
	src       types.ImageSource
	cachedimg types.Image
}

// convertedLayerInfo is the reduced form of the OCI type BlobInfo
// used in the return value of GetLayerInfo.
type convertedLayerInfo struct {
	Digest    digest.Digest `json:"digest"`
	Size      int64         `json:"size"`
	MediaType string        `json:"media_type"`
}

// mapProxyErrorCode turns an error into a known string value.
func mapProxyErrorCode(err error) proxyErrorCode {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, syscall.EPIPE):
		return proxyErrPipe
	case retry.IsErrorRetryable(err):
		return proxyErrRetryable
	default:
		return proxyErrOther
	}
}

// newProxyError creates a serializable structure for
// the client containing a mapped error code based
// on the error type, plus its value as a string.
func newProxyError(err error) proxyError {
	return proxyError{
		Code:    mapProxyErrorCode(err),
		Message: fmt.Sprintf("%v", err),
	}
}
