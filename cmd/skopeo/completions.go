package main

import (
	"github.com/containers/image/v5/tarball"
	"github.com/containers/image/v5/transports"
	"github.com/spf13/cobra"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func autocompleteTransports(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	directive := cobra.ShellCompDirectiveNoSpace
	// We still don't have a transport
	if !strings.Contains(toComplete, ":") {
		transports := supportedTransportSuggestions()
		return transports, directive | cobra.ShellCompDirectiveNoFileComp
	}
	if strings.HasPrefix(toComplete, "oci-archive:") || strings.HasPrefix(toComplete, "docker-archive:") || strings.HasPrefix(toComplete, "sif:") || strings.HasPrefix(toComplete, "oci:") {
		return nil, directive
	}
	if toComplete == "docker:" {
		return []cobra.Completion{"docker://"}, directive | cobra.ShellCompDirectiveNoFileComp
	}
	if strings.HasPrefix(toComplete, "dir:") {
		// just ShellCompDirectiveFilterDirs is more correct, but doesn't work here in bash, see https://github.com/spf13/cobra/issues/2242. Instead we get the directories ourselves.
		curDir := filepath.Dir(strings.TrimPrefix(toComplete, "dir:"))
		entries, err := os.ReadDir(curDir)
		if err != nil {
			cobra.CompErrorln("Failed ReadDir at " + curDir)
			// Fallback to whatever the shell gives us
			return nil, cobra.ShellCompDirectiveFilterDirs | cobra.ShellCompDirectiveNoFileComp
		}
		suggestions := make([]cobra.Completion, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				suggestions = append(suggestions, "dir:"+path.Join(curDir, e.Name())+"/")
			}
		}
		return suggestions, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
	}
	return nil, directive | cobra.ShellCompDirectiveNoFileComp
}

// supportedTransportSuggestions list all supported transports with the colon suffix.
func supportedTransportSuggestions() []string {
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
	return suggestions
}
