package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"go.podman.io/common/pkg/retry"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/transports/alltransports"
)

type deleteOptions struct {
	global    *globalOptions
	image     *imageOptions
	retryOpts *retry.Options
}

func deleteCmd(global *globalOptions) *cobra.Command {
	sharedFlags, sharedOpts := sharedImageFlags()
	imageFlags, imageOpts := imageFlags(global, sharedOpts, nil, "", "")
	retryFlags, retryOpts := retryFlags()
	opts := deleteOptions{
		global:    global,
		image:     imageOpts,
		retryOpts: retryOpts,
	}
	cmd := &cobra.Command{
		Use:   "delete [command options] IMAGE-NAME",
		Short: "Delete image IMAGE-NAME",
		Long: fmt.Sprintf(`Delete an "IMAGE_NAME" from a transport
Supported transports:
%s
See skopeo(1) section "IMAGE NAMES" for the expected format
`, strings.Join(transports.ListNames(), ", ")),
		RunE:              commandAction(opts.run),
		Example:           `skopeo delete docker://registry.example.com/example/pause:latest`,
		ValidArgsFunction: autocompleteImageNames,
	}
	adjustUsage(cmd)
	flags := cmd.Flags()
	flags.AddFlagSet(&sharedFlags)
	flags.AddFlagSet(&imageFlags)
	flags.AddFlagSet(&retryFlags)
	return cmd
}

func (opts *deleteOptions) run(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errorShouldDisplayUsage{errors.New("exactly one argument expected")}
	}
	imageName := args[0]

	if err := reexecIfNecessaryForImages(imageName); err != nil {
		return err
	}

	ref, err := alltransports.ParseImageName(imageName)
	if err != nil {
		return fmt.Errorf("invalid source name %s: %w", imageName, err)
	}

	sys, err := opts.image.newSystemContext()
	if err != nil {
		return err
	}

	ctx, cancel := opts.global.commandTimeoutContext()
	defer cancel()

	return retry.IfNecessary(ctx, func() error {
		return ref.DeleteImage(ctx, sys)
	}, opts.retryOpts)
}
