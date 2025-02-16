package main

import (
	"github.com/containers/image/v5/directory"
	"github.com/containers/image/v5/docker"
	dockerArchive "github.com/containers/image/v5/docker/archive"
	ociArchive "github.com/containers/image/v5/oci/archive"
	oci "github.com/containers/image/v5/oci/layout"
	"github.com/containers/image/v5/sif"
	"github.com/containers/image/v5/tarball"
	"github.com/containers/image/v5/transports"
	"github.com/spf13/cobra"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func autocompleteImageNames(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	transport, details, haveTransport := strings.Cut(toComplete, ":")
	if !haveTransport {
		transports := supportedTransportSuggestions()
		return transports, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
	}
	switch transport {
	case ociArchive.Transport.Name(), dockerArchive.Transport.Name(), sif.Transport.Name(), oci.Transport.Name():
		return nil, cobra.ShellCompDirectiveNoSpace
	case directory.Transport.Name():
		// just ShellCompDirectiveFilterDirs is more correct, but doesn't work here in bash, see https://github.com/spf13/cobra/issues/2242. Instead we get the directories ourselves.
		curDir := filepath.Dir(details)
		entries, err := os.ReadDir(curDir)
		if err != nil {
			cobra.CompErrorln("Failed ReadDir at " + curDir)
			// Fallback to whatever the shell gives us
			return nil, cobra.ShellCompDirectiveFilterDirs
		}
		suggestions := make([]cobra.Completion, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				suggestions = append(suggestions, transport+":"+path.Join(curDir, e.Name())+"/")
			}
		}
		return suggestions, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
	}
	if transport == docker.Transport.Name() && details == "" {
		return []cobra.Completion{transport + "://"}, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
	}
	return nil, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
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
