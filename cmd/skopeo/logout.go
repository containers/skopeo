package main

import (
	"io"

	"github.com/spf13/cobra"
	"go.podman.io/common/pkg/auth"
	commonFlag "go.podman.io/common/pkg/flag"
	"go.podman.io/image/v5/types"
)

type logoutOptions struct {
	global     *globalOptions
	logoutOpts auth.LogoutOptions
	tlsVerify  commonFlag.OptionalBool
}

func logoutCmd(global *globalOptions) *cobra.Command {
	opts := logoutOptions{
		global: global,
	}
	cmd := &cobra.Command{
		Use:     "logout [command options] REGISTRY",
		Short:   "Logout of a container registry",
		Long:    "Logout of a container registry on a specified server.",
		RunE:    commandAction(opts.run),
		Example: `skopeo logout quay.io`,
	}
	adjustUsage(cmd)
	flags := cmd.Flags()
	flags.AddFlagSet(auth.GetLogoutFlags(&opts.logoutOpts))
	commonFlag.OptionalBoolFlag(flags, &opts.tlsVerify, "tls-verify", "require HTTPS and verify certificates when accessing the registry")
	return cmd
}

func (opts *logoutOptions) run(args []string, stdout io.Writer) error {
	opts.logoutOpts.Stdout = stdout
	opts.logoutOpts.AcceptRepositories = true
	sys, err := opts.global.newSystemContext()
	if err != nil {
		return err
	}
	if opts.tlsVerify.Present() {
		sys.DockerInsecureSkipTLSVerify = types.NewOptionalBool(!opts.tlsVerify.Value())
	}
	return auth.Logout(sys, &opts.logoutOpts, args)
}
