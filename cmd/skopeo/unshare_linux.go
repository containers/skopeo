package main

import (
	"fmt"
	"slices"

	"github.com/containers/image/v5/transports/alltransports"
	"github.com/containers/storage/pkg/unshare"
	"github.com/moby/sys/capability"
)

var neededCapabilities = []capability.Cap{
	capability.CAP_CHOWN,
	capability.CAP_DAC_OVERRIDE,
	capability.CAP_FOWNER,
	capability.CAP_FSETID,
	capability.CAP_MKNOD,
	capability.CAP_SETFCAP,
}

func maybeReexec() error {
	// With Skopeo we need only the subset of the root capabilities necessary
	// for pulling an image to the storage.  Do not attempt to create a namespace
	// if we already have the capabilities we need.
	capabilities, err := capability.NewPid2(0)
	if err != nil {
		return fmt.Errorf("error reading the current capabilities sets: %w", err)
	}
	if err := capabilities.Load(); err != nil {
		return fmt.Errorf("error loading the current capabilities sets: %w", err)
	}
	if slices.ContainsFunc(neededCapabilities, func(cap capability.Cap) bool {
		return !capabilities.Get(capability.EFFECTIVE, cap)
	}) {
		// We miss a capability we need, create a user namespaces
		unshare.MaybeReexecUsingUserNamespace(true)
		return nil
	}
	return nil
}

func reexecIfNecessaryForImages(imageNames ...string) error {
	// Check if container-storage is used before doing unshare
	if slices.ContainsFunc(imageNames, func(imageName string) bool {
		transport := alltransports.TransportFromImageName(imageName)
		// Hard-code the storage name to avoid a reference on c/image/storage.
		// See https://github.com/containers/skopeo/issues/771#issuecomment-563125006.
		return transport != nil && transport.Name() == "containers-storage"
	}) {
		return maybeReexec()
	}
	return nil
}
