package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/containers/image/v5/directory"
	"github.com/containers/image/v5/docker"
	dockerArchive "github.com/containers/image/v5/docker/archive"
	ociArchive "github.com/containers/image/v5/oci/archive"
	oci "github.com/containers/image/v5/oci/layout"
	"github.com/containers/image/v5/sif"
	"github.com/containers/image/v5/tarball"
	"github.com/containers/image/v5/transports"
	"github.com/spf13/cobra"
)

func completionCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate completion script",
		Long: `To load completions:
Bash:
$ source <(skopeo completion bash)

# To load completions for each session, execute once:
Linux:
	$ skopeo completion bash > /etc/bash_completion.d/skopeo
MacOS:
	$ skopeo completion bash > /usr/local/etc/bash_completion.d/skopeo

Zsh:
$ echo "autoload -U compinit; compinit" >> ~/.zshrc

Fish:
$ skopeo completion fish | source

PowerShell:
$ skopeo completion powershell | Out-String | Invoke-Expression
`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Run: func(cmd *cobra.Command, args []string) {
			shell := args[0]
			switch shell {
			case "bash":
				if err := cmd.Root().GenBashCompletion(os.Stdout); err != nil {
					fmt.Println("fail to generate bash completion script", err)
					os.Exit(1)
				}
			case "zsh":
				if err := cmd.Root().GenZshCompletion(os.Stdout); err != nil {
					fmt.Println("fail to generate zsh completion script: ", err)
					os.Exit(1)
				}
			case "fish":
				if err := cmd.Root().GenFishCompletion(os.Stdout, true); err != nil {
					fmt.Println("fail to generate fish completion script: ", err)
					os.Exit(1)
				}
			case "powershell":
				if err := cmd.Root().GenPowerShellCompletion(os.Stdout); err != nil {
					fmt.Println("fail to generate powershell completion script: ", err)
					os.Exit(1)
				}
			default:
				fmt.Println("Invalid shell specified, specify bash, zsh, fish, or powershell")
				os.Exit(1)
			}
		},
	}
	return c
}

func autocompleteImageNames(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	transport, details, haveTransport := strings.Cut(toComplete, ":")
	if !haveTransport {
		transports := supportedTransportSuggestions()
		return transports, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
	}
	switch transport {
	case ociArchive.Transport.Name(), dockerArchive.Transport.Name():
		// Can have [:{*reference|@source-index}]
		// FIXME: `oci-archive:/path/to/a.oci:<TAB>` completes paths
		return nil, cobra.ShellCompDirectiveNoSpace
	case sif.Transport.Name():
		return nil, cobra.ShellCompDirectiveDefault

	// Both directory and oci should have ShellCompDirectiveFilterDirs to complete only directories, but it doesn't currently work in bash: https://github.com/spf13/cobra/issues/2242
	case oci.Transport.Name():
		// Can have '[:{reference|@source-index}]'
		// FIXME: `oci:/path/to/dir/:<TAB>` completes paths
		return nil, cobra.ShellCompDirectiveDefault | cobra.ShellCompDirectiveNoSpace
	case directory.Transport.Name():
		return nil, cobra.ShellCompDirectiveDefault

	case docker.Transport.Name():
		if details == "" {
			return []cobra.Completion{transport + "://"}, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
		}
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
