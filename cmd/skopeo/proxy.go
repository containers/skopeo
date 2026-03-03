//go:build !windows

package main

/*
 This command is still experimental. Documentation
 is available in
 docs-experimental/skopeo-experimental-image-proxy.1.md
*/

import (
	"context"
	"io"

	"github.com/spf13/cobra"
	"go.podman.io/common/pkg/json-proxy"
)

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

// Implementation of podman experimental-image-proxy using the library
func (opts *proxyOptions) run(args []string, stdout io.Writer) error {
	manager, err := jsonproxy.NewManager(
		jsonproxy.WithSystemContext(opts.imageOpts.newSystemContext),
		jsonproxy.WithPolicyContext(opts.global.getPolicyContext),
	)
	if err != nil {
		return err
	}
	defer manager.Close()
	return manager.Serve(context.Background(), opts.sockFd)
}
