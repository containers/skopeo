package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"

	"github.com/containers/image/v5/manifest"
	"github.com/spf13/cobra"
)

type manifestDigestOptions struct {
	deprecatedTLSVerify *deprecatedTLSVerifyOption
}

func manifestDigestCmd() *cobra.Command {
	deprecatedTLSVerifyFlags, deprecatedTLSVerifyOpt := deprecatedTLSVerifyFlags()
	opts := manifestDigestOptions{
		deprecatedTLSVerify: deprecatedTLSVerifyOpt,
	}
	cmd := &cobra.Command{
		Use:     "manifest-digest MANIFEST-FILE",
		Short:   "Compute a manifest digest of a file",
		RunE:    commandAction(opts.run),
		Example: "skopeo manifest-digest manifest.json",
	}
	adjustUsage(cmd)
	flags := cmd.Flags()
	flags.AddFlagSet(&deprecatedTLSVerifyFlags)
	return cmd
}

func (opts *manifestDigestOptions) run(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errors.New("Usage: skopeo manifest-digest manifest")
	}
	manifestPath := args[0]
	opts.deprecatedTLSVerify.warnIfUsed()

	man, err := ioutil.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("Error reading manifest from %s: %v", manifestPath, err)
	}
	digest, err := manifest.Digest(man)
	if err != nil {
		return fmt.Errorf("Error computing digest: %v", err)
	}
	fmt.Fprintf(stdout, "%s\n", digest)
	return nil
}
