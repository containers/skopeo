package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/containers/common/pkg/report"
	"github.com/containers/common/pkg/retry"
	"github.com/containers/image/v5/docker"
	"github.com/containers/image/v5/image"
	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/transports"
	"github.com/containers/image/v5/types"
	"github.com/containers/skopeo/cmd/skopeo/inspect"
	"github.com/docker/distribution/registry/api/errcode"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type inspectOptions struct {
	global        *globalOptions
	image         *imageOptions
	retryOpts     *retry.Options
	format        string
	raw           bool // Output the raw manifest instead of parsing information about the image
	config        bool // Output the raw config blob instead of parsing information about the image
	doNotListTags bool // Do not list all tags available in the same repository
}

func inspectCmd(global *globalOptions) *cobra.Command {
	sharedFlags, sharedOpts := sharedImageFlags()
	imageFlags, imageOpts := imageFlags(global, sharedOpts, nil, "", "")
	retryFlags, retryOpts := retryFlags()
	opts := inspectOptions{
		global:    global,
		image:     imageOpts,
		retryOpts: retryOpts,
	}
	cmd := &cobra.Command{
		Use:   "inspect [command options] IMAGE-NAME",
		Short: "Inspect image IMAGE-NAME",
		Long: fmt.Sprintf(`Return low-level information about "IMAGE-NAME" in a registry/transport
Supported transports:
%s

See skopeo(1) section "IMAGE NAMES" for the expected format
`, strings.Join(transports.ListNames(), ", ")),
		RunE: commandAction(opts.run),
		Example: `skopeo inspect docker://registry.fedoraproject.org/fedora
skopeo inspect --config docker://docker.io/alpine
skopeo inspect --format "Name: {{.Name}} Digest: {{.Digest}}" docker://registry.access.redhat.com/ubi8`,
		ValidArgsFunction: autocompleteSupportedTransports,
	}
	adjustUsage(cmd)
	flags := cmd.Flags()
	flags.BoolVar(&opts.raw, "raw", false, "output raw manifest or configuration")
	flags.BoolVar(&opts.config, "config", false, "output configuration")
	flags.StringVarP(&opts.format, "format", "f", "", "Format the output to a Go template")
	flags.BoolVarP(&opts.doNotListTags, "no-tags", "n", false, "Do not list the available tags from the repository in the output")
	flags.AddFlagSet(&sharedFlags)
	flags.AddFlagSet(&imageFlags)
	flags.AddFlagSet(&retryFlags)
	return cmd
}

func (opts *inspectOptions) run(args []string, stdout io.Writer) (retErr error) {
	var (
		rawManifest []byte
		src         types.ImageSource
		imgInspect  *types.ImageInspectInfo
	)
	ctx, cancel := opts.global.commandTimeoutContext()
	defer cancel()

	if len(args) != 1 {
		return errors.New("Exactly one argument expected")
	}
	if opts.raw && opts.format != "" {
		return errors.New("raw output does not support format option")
	}
	imageName := args[0]

	if err := reexecIfNecessaryForImages(imageName); err != nil {
		return err
	}

	sys, err := opts.image.newSystemContext()
	if err != nil {
		return err
	}

	if err := retry.IfNecessary(ctx, func() error {
		src, err = parseImageSource(ctx, opts.image, imageName)
		return err
	}, opts.retryOpts); err != nil {
		return fmt.Errorf("Error parsing image name %q: %w", imageName, err)
	}

	defer func() {
		if err := src.Close(); err != nil {
			retErr = noteCloseFailure(retErr, "closing image", err)
		}
	}()

	unparsedInstance := image.UnparsedInstance(src, nil)
	if err := retry.IfNecessary(ctx, func() error {
		rawManifest, _, err = unparsedInstance.Manifest(ctx)
		return err
	}, opts.retryOpts); err != nil {
		return fmt.Errorf("Error retrieving manifest for image: %w", err)
	}

	if opts.raw && !opts.config {
		_, err := stdout.Write(rawManifest)
		if err != nil {
			return fmt.Errorf("Error writing manifest to standard output: %w", err)
		}

		return nil
	}

	img, err := image.FromUnparsedImage(ctx, sys, unparsedInstance)
	if err != nil {
		return fmt.Errorf("Error parsing manifest for image: %w", err)
	}

	if opts.config && opts.raw {
		var configBlob []byte
		if err := retry.IfNecessary(ctx, func() error {
			configBlob, err = img.ConfigBlob(ctx)
			return err
		}, opts.retryOpts); err != nil {
			return fmt.Errorf("Error reading configuration blob: %w", err)
		}
		_, err = stdout.Write(configBlob)
		if err != nil {
			return fmt.Errorf("Error writing configuration blob to standard output: %w", err)
		}
		return nil
	} else if opts.config {
		var config *v1.Image
		if err := retry.IfNecessary(ctx, func() error {
			config, err = img.OCIConfig(ctx)
			return err
		}, opts.retryOpts); err != nil {
			return fmt.Errorf("Error reading OCI-formatted configuration data: %w", err)
		}
		if err := opts.writeOutput(stdout, config); err != nil {
			return fmt.Errorf("Error writing OCI-formatted configuration data to standard output: %w", err)
		}
		return nil
	}

	if err := retry.IfNecessary(ctx, func() error {
		imgInspect, err = img.Inspect(ctx)
		return err
	}, opts.retryOpts); err != nil {
		return err
	}

	outputData := inspect.Output{
		Name: "", // Set below if DockerReference() is known
		Tag:  imgInspect.Tag,
		// Digest is set below.
		RepoTags:      []string{}, // Possibly overridden for docker.Transport.
		Created:       imgInspect.Created,
		DockerVersion: imgInspect.DockerVersion,
		Labels:        imgInspect.Labels,
		Architecture:  imgInspect.Architecture,
		Os:            imgInspect.Os,
		Layers:        imgInspect.Layers,
		LayersData:    imgInspect.LayersData,
		Env:           imgInspect.Env,
	}
	outputData.Digest, err = manifest.Digest(rawManifest)
	if err != nil {
		return fmt.Errorf("Error computing manifest digest: %w", err)
	}
	if dockerRef := img.Reference().DockerReference(); dockerRef != nil {
		outputData.Name = dockerRef.Name()
	}
	if !opts.doNotListTags && img.Reference().Transport() == docker.Transport {
		sys, err := opts.image.newSystemContext()
		if err != nil {
			return err
		}
		outputData.RepoTags, err = docker.GetRepositoryTags(ctx, sys, img.Reference())
		if err != nil {
			// Some registries may decide to block the "list all tags" endpoint;
			// gracefully allow the inspect to continue in this case:
			fatalFailure := true
			// - AWS ECR rejects it if the "ecr:ListImages" action is not allowed.
			//   https://github.com/containers/skopeo/issues/726
			var ec errcode.ErrorCoder
			if ok := errors.As(err, &ec); ok && ec.ErrorCode() == errcode.ErrorCodeDenied {
				fatalFailure = false
			}
			// - public.ecr.aws does not implement the endpoint at all, and fails with 404:
			//   https://github.com/containers/skopeo/issues/1230
			//   This is actually "code":"NOT_FOUND", and the parser doesn’t preserve that.
			//   So, also check the error text.
			if ok := errors.As(err, &ec); ok && ec.ErrorCode() == errcode.ErrorCodeUnknown {
				var e errcode.Error
				if ok := errors.As(err, &e); ok && e.Code == errcode.ErrorCodeUnknown && e.Message == "404 page not found" {
					fatalFailure = false
				}
			}
			if fatalFailure {
				return fmt.Errorf("Error determining repository tags: %w", err)
			}
			logrus.Warnf("Registry disallows tag list retrieval; skipping")
		}
	}
	return opts.writeOutput(stdout, outputData)
}

// writeOutput writes data depending on opts.format to stdout
func (opts *inspectOptions) writeOutput(stdout io.Writer, data any) error {
	if report.IsJSON(opts.format) || opts.format == "" {
		out, err := json.MarshalIndent(data, "", "    ")
		if err == nil {
			fmt.Fprintf(stdout, "%s\n", string(out))
		}
		return err
	}

	rpt, err := report.New(stdout, "skopeo inspect").Parse(report.OriginUser, opts.format)
	if err != nil {
		return err
	}
	defer rpt.Flush()
	return rpt.Execute([]any{data})
}
