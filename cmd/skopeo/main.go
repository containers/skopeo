package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/containers/image/v5/signature"
	"github.com/containers/image/v5/types"
	"github.com/containers/skopeo/version"
	"github.com/containers/storage/pkg/reexec"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// gitCommit will be the hash that the binary was built from
// and will be populated by the Makefile
var gitCommit = ""

var defaultUserAgent = "skopeo/" + version.Version

type globalOptions struct {
	debug              bool          // Enable debug output
	tlsVerify          optionalBool  // Require HTTPS and verify certificates (for docker: and docker-daemon:)
	policyPath         string        // Path to a signature verification policy file
	insecurePolicy     bool          // Use an "allow everything" signature verification policy
	registriesDirPath  string        // Path to a "registries.d" registry configuration directory
	overrideArch       string        // Architecture to use for choosing images, instead of the runtime one
	overrideOS         string        // OS to use for choosing images, instead of the runtime one
	overrideVariant    string        // Architecture variant to use for choosing images, instead of the runtime one
	commandTimeout     time.Duration // Timeout for the command execution
	registriesConfPath string        // Path to the "registries.conf" file
	tmpDir             string        // Path to use for big temporary files
}

// createApp returns a cobra.Command, and the underlying globalOptions object, to be run or tested.
// unfilteredHelp should always be false, it only exists to test rootHelpFunc.
func createApp(unfilteredHelp bool) (*cobra.Command, *globalOptions) {
	opts := globalOptions{}

	rootCommand := &cobra.Command{
		Use:  "skopeo",
		Long: "Various operations with container images and container image registries",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return opts.before(cmd)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		// Currently, skopeo uses manually written completions.  Cobra allows
		// for auto-generating completions for various shells.  Podman is
		// already making us of that.  If Skopeo decides to follow, please
		// remove the line below (and hide the `completion` command).
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}
	if gitCommit != "" {
		rootCommand.Version = fmt.Sprintf("%s commit: %s", version.Version, gitCommit)
	} else {
		rootCommand.Version = version.Version
	}
	// Override default `--version` global flag to enable `-v` shorthand
	var dummyVersion bool
	rootCommand.Flags().BoolVarP(&dummyVersion, "version", "v", false, "Version for Skopeo")
	rootCommand.PersistentFlags().BoolVar(&opts.debug, "debug", false, "enable debug output")
	// This flag is hidden from top-level --help via rootHelpFunc
	optionalBoolFlag(rootCommand.PersistentFlags(), &opts.tlsVerify, "tls-verify", "Require HTTPS and verify certificates when accessing the registry")
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
	rootCommand.AddCommand(
		copyCmd(&opts),
		deleteCmd(&opts),
		inspectCmd(&opts),
		layersCmd(&opts),
		loginCmd(&opts),
		logoutCmd(&opts),
		manifestDigestCmd(),
		syncCmd(&opts),
		standaloneSignCmd(),
		standaloneVerifyCmd(),
		tagsCmd(&opts),
		untrustedSignatureDumpCmd(),
	)
	if !unfilteredHelp {
		rootCommand.SetHelpFunc(rootHelpFunc(rootCommand))
	}
	return rootCommand, &opts
}

// rootHelpFunc implements (skopeo --help).
// We want to hide the deprecated global --tls-verify command from help, and we can't do it
// by just marking it hidden, because that marks the local --tls-verify (e.g. skopeo inspect --tls-verify)
// hidden as well.
// So we just filter it out here.
func rootHelpFunc(rootCommand *cobra.Command) func(*cobra.Command, []string) {
	reentered := false
	return func(c *cobra.Command, a []string) {
		if reentered {
			// We don't plan for this to happen.
			panic("Internal error: rootHelpFunc's closure was reentered")
		}
		reentered = true

		// The function set by SetHelpFunc is also called by (skopeo subcommand --help); in that
		// case we don't want to modify anything.
		filterTLSVerify := c == rootCommand
		logrus.Errorf("rootHelpFunc %v %v", filterTLSVerify, a)

		// We expect to be only called once; so we can just revert to the default implementation
		// here ...
		rootCommand.SetHelpFunc(nil) // Note that nil is not quite a documented valid value
		// .. and then obtain the built-in HelpFunc implementation, which is not available
		// as a named method.
		defaultHelpFunc := rootCommand.HelpFunc()

		// Again, we expect to be called once; so, destructively redirect the output of the
		// --help implementation
		realOut := c.OutOrStdout()
		buf := bytes.Buffer{}
		c.SetOut(&buf)

		// Collect the default --help output...
		defaultHelpFunc(c, a)
		// ... and modify it
		res := bytes.Buffer{}
		for {
			line, err := buf.ReadString('\n')
			if err == io.EOF {
				break
			}
			logrus.Errorf("Line %v", line)
			if err != nil {
				c.PrintErrln(fmt.Sprintf("Error preparing --help output from a buffer: %v", err.Error()))
				return
			}
			if !filterTLSVerify || !strings.Contains(line, "--tls-verify") {
				res.WriteString(line)
			} else {
				logrus.Errorf("IGNORED %v", line)
			}
		}
		if _, err := realOut.Write(res.Bytes()); err != nil {
			c.PrintErrln(err)
		}
	}
}

// before is run by the cli package for any command, before running the command-specific handler.
func (opts *globalOptions) before(cmd *cobra.Command) error {
	if opts.debug {
		logrus.SetLevel(logrus.DebugLevel)
	}
	if opts.tlsVerify.present {
		logrus.Warn("'--tls-verify' is deprecated, please set this on the specific subcommand")
	}
	return nil
}

func main() {
	if reexec.Init() {
		return
	}
	rootCmd, _ := createApp(false)
	if err := rootCmd.Execute(); err != nil {
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
	ctx := &types.SystemContext{
		RegistriesDirPath:        opts.registriesDirPath,
		ArchitectureChoice:       opts.overrideArch,
		OSChoice:                 opts.overrideOS,
		VariantChoice:            opts.overrideVariant,
		SystemRegistriesConfPath: opts.registriesConfPath,
		BigFilesTemporaryDir:     opts.tmpDir,
		DockerRegistryUserAgent:  defaultUserAgent,
	}
	// DEPRECATED: We support this for backward compatibility, but override it if a per-image flag is provided.
	if opts.tlsVerify.present {
		ctx.DockerInsecureSkipTLSVerify = types.NewOptionalBool(!opts.tlsVerify.value)
	}
	return ctx
}
