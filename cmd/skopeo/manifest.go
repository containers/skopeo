package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/containers/image/v5/manifest"
	"github.com/spf13/cobra"
)

type manifestDigestOptions struct {
}

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
		return errors.New("Usage: skopeo manifest-digest manifest")
	}
	manifestPath := args[0]

	man, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("Error reading manifest from %s: %w", manifestPath, err)
	}
	digest, err := manifest.Digest(man)
	if err != nil {
		return fmt.Errorf("Error computing digest: %w", err)
	}
	fmt.Fprintf(stdout, "%s\n", digest)
	return nil
}
