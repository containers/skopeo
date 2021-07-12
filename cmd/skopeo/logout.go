package main

import (
	"io"

	"github.com/containers/common/pkg/auth"
	"github.com/spf13/cobra"
)

type logoutOptions struct {
	global              *globalOptions
	deprecatedTLSVerify *deprecatedTLSVerifyOption
	logoutOpts          auth.LogoutOptions
}

func logoutCmd(global *globalOptions) *cobra.Command {
	deprecatedTLSVerifyFlags, deprecatedTLSVerifyOpt := deprecatedTLSVerifyFlags()
	opts := logoutOptions{
		global:              global,
		deprecatedTLSVerify: deprecatedTLSVerifyOpt,
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
	flags.AddFlagSet(&deprecatedTLSVerifyFlags)
	flags.AddFlagSet(auth.GetLogoutFlags(&opts.logoutOpts))
	return cmd
}

func (opts *logoutOptions) run(args []string, stdout io.Writer) error {
	opts.deprecatedTLSVerify.warnIfUsed()

	opts.logoutOpts.Stdout = stdout
	sys := opts.global.newSystemContext()
	return auth.Logout(sys, &opts.logoutOpts, args)
}
