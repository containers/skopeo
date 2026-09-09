package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"go.podman.io/image/v5/manifest"
)

type manifestDigestOptions struct{}

func manifestDigestCmd() *cobra.Command {
	var opts manifestDigestOptions
	cmd := &cobra.Command{
		Use:     "manifest-digest MANIFEST-FILE",
		Short:   "Compute a manifest digest of a file",
		RunE:    commandAction(opts.run),
		Example: "skopeo manifest-digest manifest.json",
	}
	adjustUsage(cmd)
	return cmd
}

func (opts *manifestDigestOptions) run(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errorShouldDisplayUsage{errors.New("exactly one argument expected")}
	}
	manifestPath := args[0]

	man, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("reading manifest from %s: %w", manifestPath, err)
	}
	digest, err := manifest.Digest(man)
	if err != nil {
		return fmt.Errorf("computing digest: %w", err)
	}
	fmt.Fprintf(stdout, "%s\n", digest)
	return nil
}
