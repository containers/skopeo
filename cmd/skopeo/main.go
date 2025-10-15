package main

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/containers/skopeo/version"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	commonFlag "go.podman.io/common/pkg/flag"
	"go.podman.io/image/v5/signature"
	"go.podman.io/image/v5/types"
	"go.podman.io/storage/pkg/reexec"
)

var defaultUserAgent = "skopeo/" + version.Version

type globalOptions struct {
	debug              bool                    // Enable debug output
	tlsVerify          commonFlag.OptionalBool // Require HTTPS and verify certificates (for docker: and docker-daemon:)
	policyPath         string                  // Path to a signature verification policy file
	insecurePolicy     bool                    // Use an "allow everything" signature verification policy
	registriesDirPath  string                  // Path to a "registries.d" registry configuration directory
	overrideArch       string                  // Architecture to use for choosing images, instead of the runtime one
	overrideOS         string                  // OS to use for choosing images, instead of the runtime one
	overrideVariant    string                  // Architecture variant to use for choosing images, instead of the runtime one
	commandTimeout     time.Duration           // Timeout for the command execution
	registriesConfPath string                  // Path to the "registries.conf" file
	tmpDir             string                  // Path to use for big temporary files
	userAgentPrefix    string                  // Prefix to add to the user agent string
}

// requireSubcommand returns an error if no sub command is provided
// This was copied from podman: `github.com/containers/podman/cmd/podman/validate/args.go
// Some small style changes to match skopeo were applied, but try to apply any
// bugfixes there first.
func requireSubcommand(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		suggestions := cmd.SuggestionsFor(args[0])
		if len(suggestions) == 0 {
			return fmt.Errorf("Unrecognized command `%[1]s %[2]s`\nTry '%[1]s --help' for more information", cmd.CommandPath(), args[0])
		}
		return fmt.Errorf("Unrecognized command `%[1]s %[2]s`\n\nDid you mean this?\n\t%[3]s\n\nTry '%[1]s --help' for more information", cmd.CommandPath(), args[0], strings.Join(suggestions, "\n\t"))
	}
	return fmt.Errorf("Missing command '%[1]s COMMAND'\nTry '%[1]s --help' for more information", cmd.CommandPath())
}

// createApp returns a cobra.Command, and the underlying globalOptions object, to be run or tested.
func createApp() (*cobra.Command, *globalOptions) {
	opts := globalOptions{}

	rootCommand := &cobra.Command{
		Use:               "skopeo",
		Long:              "Various operations with container images and container image registries",
		RunE:              requireSubcommand,
		PersistentPreRunE: opts.before,
		SilenceUsage:      true,
		SilenceErrors:     true,
		// Hide the completion command which is provided by cobra
		CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
		// This is documented to parse "local" (non-PersistentFlags) flags of parent commands before
		// running subcommands and handling their options. We don't really run into such cases,
		// because all of our flags on rootCommand are in PersistentFlags, except for the deprecated --tls-verify;
		// in that case we need TraverseChildren so that we can distinguish between
		// (skopeo --tls-verify inspect) (causes a warning) and (skopeo inspect --tls-verify) (no warning).
		TraverseChildren: true,
	}
	// We don’t use debug.ReadBuildInfo to automate version.Version, because that would not work well for builds from
	// a released tarball (e.g. RPM builds).
	if commit := gitCommit(); commit != "" {
		rootCommand.Version = fmt.Sprintf("%s commit: %s", version.Version, commit)
	} else {
		rootCommand.Version = version.Version
	}
	// Override default `--version` global flag to enable `-v` shorthand
	var dummyVersion bool
	rootCommand.Flags().BoolVarP(&dummyVersion, "version", "v", false, "Version for Skopeo")
	rootCommand.PersistentFlags().BoolVar(&opts.debug, "debug", false, "enable debug output")
	rootCommand.PersistentFlags().StringVar(&opts.policyPath, "policy", "", "Path to a trust policy file")
	rootCommand.PersistentFlags().BoolVar(&opts.insecurePolicy, "insecure-policy", false, "run the tool without any policy check")
	rootCommand.PersistentFlags().StringVar(&opts.registriesDirPath, "registries.d", "", "use registry configuration files in `DIR` (e.g. for container signature storage)")
	rootCommand.PersistentFlags().StringVar(&opts.overrideArch, "override-arch", "", "use `ARCH` instead of the architecture of the machine for choosing images")
	rootCommand.PersistentFlags().StringVar(&opts.overrideOS, "override-os", "", "use `OS` instead of the running OS for choosing images")
	rootCommand.PersistentFlags().StringVar(&opts.overrideVariant, "override-variant", "", "use `VARIANT` instead of the running architecture variant for choosing images")
	rootCommand.PersistentFlags().DurationVar(&opts.commandTimeout, "command-timeout", 0, "timeout for the command execution")
	rootCommand.PersistentFlags().StringVar(&opts.registriesConfPath, "registries-conf", "", "path to the registries.conf file")
	if err := rootCommand.PersistentFlags().MarkHidden("registries-conf"); err != nil {
		logrus.Fatal("unable to mark registries-conf flag as hidden")
	}
	rootCommand.PersistentFlags().StringVar(&opts.tmpDir, "tmpdir", "", "directory used to store temporary files")
	rootCommand.PersistentFlags().StringVar(&opts.userAgentPrefix, "user-agent-prefix", "", "prefix to add to the user agent string")
	flag := commonFlag.OptionalBoolFlag(rootCommand.Flags(), &opts.tlsVerify, "tls-verify", "Require HTTPS and verify certificates when accessing the registry")
	flag.Hidden = true
	rootCommand.AddCommand(
		copyCmd(&opts),
		deleteCmd(&opts),
		generateSigstoreKeyCmd(),
		inspectCmd(&opts),
		layersCmd(&opts),
		loginCmd(&opts),
		logoutCmd(&opts),
		manifestDigestCmd(),
		proxyCmd(&opts),
		syncCmd(&opts),
		standaloneSignCmd(),
		standaloneVerifyCmd(),
		tagsCmd(&opts),
		untrustedSignatureDumpCmd(),
	)
	return rootCommand, &opts
}

// gitCommit returns the git commit for this codebase, if we are built from a git repo; "" otherwise.
func gitCommit() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		logrus.Fatal("runtime.ReadBuildInfo failed")
	}
	for _, e := range bi.Settings {
		if e.Key == "vcs.revision" {
			return e.Value
		}
	}
	return ""
}

// before is run by the cli package for any command, before running the command-specific handler.
func (opts *globalOptions) before(cmd *cobra.Command, args []string) error {
	if opts.debug {
		logrus.SetLevel(logrus.DebugLevel)
	}
	if opts.tlsVerify.Present() {
		logrus.Warn("'--tls-verify' is deprecated, please set this on the specific subcommand")
	}
	return nil
}

func main() {
	if reexec.Init() {
		return
	}
	rootCmd, _ := createApp()
	if err := rootCmd.Execute(); err != nil {
		if isNotFoundImageError(err) {
			logrus.StandardLogger().Log(logrus.FatalLevel, err)
			logrus.Exit(2)
		}
		logrus.Fatal(err)
	}
}

// getPolicyContext returns a *signature.PolicyContext based on opts.
func (opts *globalOptions) getPolicyContext() (*signature.PolicyContext, error) {
	var policy *signature.Policy // This could be cached across calls in opts.
	var err error
	if opts.insecurePolicy {
		policy = &signature.Policy{Default: []signature.PolicyRequirement{signature.NewPRInsecureAcceptAnything()}}
	} else if opts.policyPath == "" {
		policy, err = signature.DefaultPolicy(nil)
	} else {
		policy, err = signature.NewPolicyFromFile(opts.policyPath)
	}
	if err != nil {
		return nil, err
	}
	return signature.NewPolicyContext(policy)
}

// commandTimeoutContext returns a context.Context and a cancellation callback based on opts.
// The caller should usually "defer cancel()" immediately after calling this.
func (opts *globalOptions) commandTimeoutContext() (context.Context, context.CancelFunc) {
	ctx := context.Background()
	var cancel context.CancelFunc = func() {}
	if opts.commandTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, opts.commandTimeout)
	}
	return ctx, cancel
}

// newSystemContext returns a *types.SystemContext corresponding to opts.
// It is guaranteed to return a fresh instance, so it is safe to make additional updates to it.
func (opts *globalOptions) newSystemContext() *types.SystemContext {
	userAgent := defaultUserAgent
	if opts.userAgentPrefix != "" {
		userAgent = opts.userAgentPrefix + " " + defaultUserAgent
	}
	ctx := &types.SystemContext{
		RegistriesDirPath:        opts.registriesDirPath,
		ArchitectureChoice:       opts.overrideArch,
		OSChoice:                 opts.overrideOS,
		VariantChoice:            opts.overrideVariant,
		SystemRegistriesConfPath: opts.registriesConfPath,
		BigFilesTemporaryDir:     opts.tmpDir,
		DockerRegistryUserAgent:  userAgent,
	}
	// DEPRECATED: We support this for backward compatibility, but override it if a per-image flag is provided.
	if opts.tlsVerify.Present() {
		ctx.DockerInsecureSkipTLSVerify = types.NewOptionalBool(!opts.tlsVerify.Value())
	}
	return ctx
}
