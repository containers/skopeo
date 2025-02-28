package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/containers/common/pkg/retry"
	"github.com/containers/image/v5/directory"
	"github.com/containers/image/v5/image"
	"github.com/containers/image/v5/pkg/blobinfocache"
	"github.com/containers/image/v5/types"
	"github.com/opencontainers/go-digest"
	"github.com/spf13/cobra"
)

type layersOptions struct {
	global    *globalOptions
	image     *imageOptions
	retryOpts *retry.Options
}

func layersCmd(global *globalOptions) *cobra.Command {
	sharedFlags, sharedOpts := sharedImageFlags()
	imageFlags, imageOpts := imageFlags(global, sharedOpts, nil, "", "")
	retryFlags, retryOpts := retryFlags()
	opts := layersOptions{
		global:    global,
		image:     imageOpts,
		retryOpts: retryOpts,
	}
	cmd := &cobra.Command{
		Hidden: true,
		Use:    "layers [command options] IMAGE-NAME [LAYER...]",
		Short:  "Get layers of IMAGE-NAME",
		RunE:   commandAction(opts.run),
	}
	adjustUsage(cmd)
	flags := cmd.Flags()
	flags.AddFlagSet(&sharedFlags)
	flags.AddFlagSet(&imageFlags)
	flags.AddFlagSet(&retryFlags)
	return cmd
}

func (opts *layersOptions) run(args []string, stdout io.Writer) (retErr error) {
	fmt.Fprintln(os.Stderr, `DEPRECATED: skopeo layers is deprecated in favor of skopeo copy`)
	if len(args) == 0 {
		return errors.New("Usage: layers imageReference [layer...]")
	}
	imageName := args[0]

	if err := reexecIfNecessaryForImages(imageName); err != nil {
		return err
	}

	ctx, cancel := opts.global.commandTimeoutContext()
	defer cancel()

	sys, err := opts.image.newSystemContext()
	if err != nil {
		return err
	}
	cache := blobinfocache.DefaultCache(sys)
	var (
		rawSource types.ImageSource
		src       types.ImageCloser
	)
	if err = retry.IfNecessary(ctx, func() error {
		rawSource, err = parseImageSource(ctx, opts.image, imageName)
		return err
	}, opts.retryOpts); err != nil {
		return err
	}
	if err = retry.IfNecessary(ctx, func() error {
		src, err = image.FromSource(ctx, sys, rawSource)
		return err
	}, opts.retryOpts); err != nil {
		if closeErr := rawSource.Close(); closeErr != nil {
			return fmt.Errorf("%w (closing image source: %v)", err, closeErr)
		}

		return err
	}
	defer func() {
		if err := src.Close(); err != nil {
			retErr = noteCloseFailure(retErr, "closing image", err)
		}
	}()

	type blobDigest struct {
		digest   digest.Digest
		isConfig bool
	}
	var blobDigests []blobDigest
	for _, dString := range args[1:] {
		if !strings.HasPrefix(dString, "sha256:") {
			dString = "sha256:" + dString
		}
		d, err := digest.Parse(dString)
		if err != nil {
			return err
		}
		blobDigests = append(blobDigests, blobDigest{digest: d, isConfig: false})
	}

	if len(blobDigests) == 0 {
		layers := src.LayerInfos()
		seenLayers := map[digest.Digest]struct{}{}
		for _, info := range layers {
			if _, ok := seenLayers[info.Digest]; !ok {
				blobDigests = append(blobDigests, blobDigest{digest: info.Digest, isConfig: false})
				seenLayers[info.Digest] = struct{}{}
			}
		}
		configInfo := src.ConfigInfo()
		if configInfo.Digest != "" {
			blobDigests = append(blobDigests, blobDigest{digest: configInfo.Digest, isConfig: true})
		}
	}

	tmpDir, err := os.MkdirTemp(".", "layers-")
	if err != nil {
		return err
	}
	tmpDirRef, err := directory.NewReference(tmpDir)
	if err != nil {
		return err
	}
	dest, err := tmpDirRef.NewImageDestination(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		if err := dest.Close(); err != nil {
			retErr = noteCloseFailure(retErr, "closing destination", err)
		}
	}()

	for _, bd := range blobDigests {
		var (
			r        io.ReadCloser
			blobSize int64
		)
		if err = retry.IfNecessary(ctx, func() error {
			r, blobSize, err = rawSource.GetBlob(ctx, types.BlobInfo{Digest: bd.digest, Size: -1}, cache)
			return err
		}, opts.retryOpts); err != nil {
			return err
		}
		defer func() {
			if err := r.Close(); err != nil {
				retErr = noteCloseFailure(retErr, fmt.Sprintf("closing blob %q", bd.digest.String()), err)
			}
		}()
		if _, err := dest.PutBlob(ctx, r, types.BlobInfo{Digest: bd.digest, Size: blobSize}, cache, bd.isConfig); err != nil {
			return err
		}
	}

	var manifest []byte
	if err = retry.IfNecessary(ctx, func() error {
		manifest, _, err = src.Manifest(ctx)
		return err
	}, opts.retryOpts); err != nil {
		return err
	}
	if err := dest.PutManifest(ctx, manifest, nil); err != nil {
		return err
	}

	return dest.Commit(ctx, image.UnparsedInstance(rawSource, nil))
}
