package main

import (
	"github.com/containers/image/v5/tarball"
	"github.com/containers/image/v5/transports"
	"github.com/spf13/cobra"
)

// autocompleteSupportedTransports list all supported transports with the colon suffix.
func autocompleteSupportedTransports(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	tps := transports.ListNames()
	suggestions := make([]cobra.Completion, 0, len(tps))
	for _, tp := range tps {
		// ListNames is generally expected to filter out deprecated transports.
		// tarball: is not deprecated, but it is only usable from a Go caller (using tarball.ConfigUpdater),
		// so don’t offer it on the CLI.
		if tp != tarball.Transport.Name() {
			suggestions = append(suggestions, tp+":")
		}
	}
	return suggestions, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}
