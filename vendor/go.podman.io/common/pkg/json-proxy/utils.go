package jsonproxy

import (
	"errors"
	"fmt"

	dockerdistributionerrcode "github.com/docker/distribution/registry/api/errcode"
	dockerdistributionapi "github.com/docker/distribution/registry/api/v2"
	ociarchive "go.podman.io/image/v5/oci/archive"
	ocilayout "go.podman.io/image/v5/oci/layout"
	"go.podman.io/image/v5/storage"
)

// noteCloseFailure helps with handling close errors in defer statements.
func noteCloseFailure(err error, description string, closeErr error) error {
	// We don't accept a Closer() and close it ourselves because signature.PolicyContext has .Destroy(), not .Close().
	// This also makes it harder for a caller to do
	//     defer noteCloseFailure(returnedErr, …)
	// which doesn't use the right value of returnedErr, and doesn't update it.
	if err == nil {
		return fmt.Errorf("%s: %w", description, closeErr)
	}
	// In this case we prioritize the primary error for use with %w; closeErr is usually less relevant, or might be a consequence of the primary error.
	return fmt.Errorf("%w (%s: %v)", err, description, closeErr)
}

// isNotFoundImageError checks if an error indicates that an image was not found.
func isNotFoundImageError(err error) bool {
	var layoutImageNotFoundError ocilayout.ImageNotFoundError
	var archiveImageNotFoundError ociarchive.ImageNotFoundError
	return isDockerManifestUnknownError(err) ||
		errors.Is(err, storage.ErrNoSuchImage) ||
		errors.As(err, &layoutImageNotFoundError) ||
		errors.As(err, &archiveImageNotFoundError)
}

// isDockerManifestUnknownError checks if an error is a Docker manifest unknown error.
func isDockerManifestUnknownError(err error) bool {
	var ec dockerdistributionerrcode.ErrorCoder
	if !errors.As(err, &ec) {
		return false
	}
	return ec.ErrorCode() == dockerdistributionapi.ErrorCodeManifestUnknown
}
