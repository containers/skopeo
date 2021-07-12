package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/containers/common/pkg/retry"
	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/pkg/compression"
	"github.com/containers/image/v5/transports/alltransports"
	"github.com/containers/image/v5/types"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// errorShouldDisplayUsage is a subtype of error used by command handlers to indicate that cli.ShowSubcommandHelp should be called.
type errorShouldDisplayUsage struct {
	error
}

// commandAction intermediates between the RunE interface and the real handler,
// primarily to ensure that cobra.Command is not available to the handler, which in turn
// makes sure that the cmd.Flags() etc. flag access functions are not used,
// and everything is done using the *Options structures and the *Var() methods of cmd.Flag().
// handler may return errorShouldDisplayUsage to cause c.Help to be called.
func commandAction(handler func(args []string, stdout io.Writer) error) func(cmd *cobra.Command, args []string) error {
	return func(c *cobra.Command, args []string) error {
		err := handler(args, c.OutOrStdout())
		if _, ok := err.(errorShouldDisplayUsage); ok {
			c.Help()
		}
		return err
	}
}

// deprecatedTLSVerifyOption represents a deprecated --tls-verify option,
// which was accepted for all subcommands, for a time.
// Every user should call deprecatedTLSVerifyOption.warnIfUsed() as part of handling the CLI,
// regardless if the value actually ends up being used.
// DO NOT ADD ANY NEW USES OF THIS; just call dockerImageFlags with an appropriate, possibly empty, flagPrefix.
type deprecatedTLSVerifyOption struct {
	tlsVerify optionalBool // FIXME FIXME: Warn if this is used, or even if it is ignored.
}

// Warn if tlsVerify was set by the user; it was historically accepted
// for this subcommand, but it makes no sense and it is ignored.
func (opts *deprecatedTLSVerifyOption) warnIfUsed() {
	if opts.tlsVerify.present {
		logrus.Warn("'--tls-verify' has no effect on this subcommand and is deprecated")
	}
}

// deprecatedTLSVerifyFlags prepares the CLI flag writing into deprecatedTLSVerifyOption, and the managed deprecatedTLSVerifyOption structure.
// DO NOT ADD ANY NEW USES OF THIS; just call dockerImageFlags with an appropriate, possibly empty, flagPrefix.
func deprecatedTLSVerifyFlags() (pflag.FlagSet, *deprecatedTLSVerifyOption) {
	opts := deprecatedTLSVerifyOption{}
	fs := pflag.FlagSet{}
	flag := optionalBoolFlag(&fs, &opts.tlsVerify, "tls-verify", "require HTTPS and verify certificates when talking to the container registry or daemon (defaults to true)")
	flag.Hidden = true
	return fs, &opts
}

// sharedImageOptions collects CLI flags which are image-related, but do not change across images.
// This really should be a part of globalOptions, but that would break existing users of (skopeo copy --authfile=).
type sharedImageOptions struct {
	authFilePath string // Path to a */containers/auth.json
}

// sharedImageFlags prepares a collection of CLI flags writing into sharedImageOptions, and the managed sharedImageOptions structure.
func sharedImageFlags() (pflag.FlagSet, *sharedImageOptions) {
	opts := sharedImageOptions{}
	fs := pflag.FlagSet{}
	fs.StringVar(&opts.authFilePath, "authfile", os.Getenv("REGISTRY_AUTH_FILE"), "path of the authentication file. Default is ${XDG_RUNTIME_DIR}/containers/auth.json")
	return fs, &opts
}

// dockerImageOptions collects CLI flags specific to the "docker" transport, which are
// the same across subcommands, but may be different for each image
// (e.g. may differ between the source and destination of a copy)
type dockerImageOptions struct {
	global              *globalOptions             // May be shared across several imageOptions instances.
	shared              *sharedImageOptions        // May be shared across several imageOptions instances.
	deprecatedTLSVerify *deprecatedTLSVerifyOption // May be shared across several imageOptions instances, or nil.
	authFilePath        optionalString             // Path to a */containers/auth.json (prefixed version to override shared image option).
	credsOption         optionalString             // username[:password] for accessing a registry
	registryToken       optionalString             // token to be used directly as a Bearer token when accessing the registry
	dockerCertPath      string                     // A directory using Docker-like *.{crt,cert,key} files for connecting to a registry or a daemon
	tlsVerify           optionalBool               // Require HTTPS and verify certificates (for docker: and docker-daemon:)
	noCreds             bool                       // Access the registry anonymously
}

// imageOptions collects CLI flags which are the same across subcommands, but may be different for each image
// (e.g. may differ between the source and destination of a copy)
type imageOptions struct {
	dockerImageOptions
	sharedBlobDir    string // A directory to use for OCI blobs, shared across repositories
	dockerDaemonHost string // docker-daemon: host to connect to
}

// dockerImageFlags prepares a collection of docker-transport specific CLI flags
// writing into imageOptions, and the managed imageOptions structure.
func dockerImageFlags(global *globalOptions, shared *sharedImageOptions, deprecatedTLSVerify *deprecatedTLSVerifyOption, flagPrefix, credsOptionAlias string) (pflag.FlagSet, *imageOptions) {
	flags := imageOptions{
		dockerImageOptions: dockerImageOptions{
			global:              global,
			shared:              shared,
			deprecatedTLSVerify: deprecatedTLSVerify,
		},
	}

	fs := pflag.FlagSet{}
	if flagPrefix != "" {
		// the non-prefixed flag is handled by a shared flag.
		fs.Var(newOptionalStringValue(&flags.authFilePath), flagPrefix+"authfile", "path of the authentication file. Default is ${XDG_RUNTIME_DIR}/containers/auth.json")
	}
	fs.Var(newOptionalStringValue(&flags.credsOption), flagPrefix+"creds", "Use `USERNAME[:PASSWORD]` for accessing the registry")
	if credsOptionAlias != "" {
		// This is horribly ugly, but we need to support the old option forms of (skopeo copy) for compatibility.
		// Don't add any more cases like this.
		f := fs.VarPF(newOptionalStringValue(&flags.credsOption), credsOptionAlias, "", "Use `USERNAME[:PASSWORD]` for accessing the registry")
		f.Hidden = true
	}
	fs.Var(newOptionalStringValue(&flags.registryToken), flagPrefix+"registry-token", "Provide a Bearer token for accessing the registry")
	fs.StringVar(&flags.dockerCertPath, flagPrefix+"cert-dir", "", "use certificates at `PATH` (*.crt, *.cert, *.key) to connect to the registry or daemon")
	optionalBoolFlag(&fs, &flags.tlsVerify, flagPrefix+"tls-verify", "require HTTPS and verify certificates when talking to the container registry or daemon (defaults to true)")
	fs.BoolVar(&flags.noCreds, flagPrefix+"no-creds", false, "Access the registry anonymously")
	return fs, &flags
}

// imageFlags prepares a collection of CLI flags writing into imageOptions, and the managed imageOptions structure.
func imageFlags(global *globalOptions, shared *sharedImageOptions, deprecatedTLSVerify *deprecatedTLSVerifyOption, flagPrefix, credsOptionAlias string) (pflag.FlagSet, *imageOptions) {
	dockerFlags, opts := dockerImageFlags(global, shared, deprecatedTLSVerify, flagPrefix, credsOptionAlias)

	fs := pflag.FlagSet{}
	fs.StringVar(&opts.sharedBlobDir, flagPrefix+"shared-blob-dir", "", "`DIRECTORY` to use to share blobs across OCI repositories")
	fs.StringVar(&opts.dockerDaemonHost, flagPrefix+"daemon-host", "", "use docker daemon host at `HOST` (docker-daemon: only)")
	fs.AddFlagSet(&dockerFlags)
	return fs, opts
}

func retryFlags() (pflag.FlagSet, *retry.RetryOptions) {
	opts := retry.RetryOptions{}
	fs := pflag.FlagSet{}
	fs.IntVar(&opts.MaxRetry, "retry-times", 0, "the number of times to possibly retry")
	return fs, &opts
}

// newSystemContext returns a *types.SystemContext corresponding to opts.
// It is guaranteed to return a fresh instance, so it is safe to make additional updates to it.
func (opts *imageOptions) newSystemContext() (*types.SystemContext, error) {
	// *types.SystemContext instance from globalOptions
	//  imageOptions option overrides the instance if both are present.
	ctx := opts.global.newSystemContext()
	ctx.DockerCertPath = opts.dockerCertPath
	ctx.OCISharedBlobDirPath = opts.sharedBlobDir
	ctx.AuthFilePath = opts.shared.authFilePath
	ctx.DockerDaemonHost = opts.dockerDaemonHost
	ctx.DockerDaemonCertPath = opts.dockerCertPath
	if opts.dockerImageOptions.authFilePath.present {
		ctx.AuthFilePath = opts.dockerImageOptions.authFilePath.value
	}
	if opts.deprecatedTLSVerify != nil && opts.deprecatedTLSVerify.tlsVerify.present {
		// If both this deprecated option and a non-deprecated option is present, we use the latter value.
		ctx.DockerInsecureSkipTLSVerify = types.NewOptionalBool(!opts.deprecatedTLSVerify.tlsVerify.value)
	}
	if opts.tlsVerify.present {
		ctx.DockerDaemonInsecureSkipTLSVerify = !opts.tlsVerify.value
	}
	if opts.tlsVerify.present {
		ctx.DockerInsecureSkipTLSVerify = types.NewOptionalBool(!opts.tlsVerify.value)
	}
	if opts.credsOption.present && opts.noCreds {
		return nil, errors.New("creds and no-creds cannot be specified at the same time")
	}
	if opts.credsOption.present {
		var err error
		ctx.DockerAuthConfig, err = getDockerAuth(opts.credsOption.value)
		if err != nil {
			return nil, err
		}
	}
	if opts.registryToken.present {
		ctx.DockerBearerRegistryToken = opts.registryToken.value
	}
	if opts.noCreds {
		ctx.DockerAuthConfig = &types.DockerAuthConfig{}
	}

	return ctx, nil
}

// imageDestOptions is a superset of imageOptions specialized for image destinations.
type imageDestOptions struct {
	*imageOptions
	dirForceCompression         bool        // Compress layers when saving to the dir: transport
	ociAcceptUncompressedLayers bool        // Whether to accept uncompressed layers in the oci: transport
	compressionFormat           string      // Format to use for the compression
	compressionLevel            optionalInt // Level to use for the compression
}

// imageDestFlags prepares a collection of CLI flags writing into imageDestOptions, and the managed imageDestOptions structure.
func imageDestFlags(global *globalOptions, shared *sharedImageOptions, deprecatedTLSVerify *deprecatedTLSVerifyOption, flagPrefix, credsOptionAlias string) (pflag.FlagSet, *imageDestOptions) {
	genericFlags, genericOptions := imageFlags(global, shared, deprecatedTLSVerify, flagPrefix, credsOptionAlias)
	opts := imageDestOptions{imageOptions: genericOptions}
	fs := pflag.FlagSet{}
	fs.AddFlagSet(&genericFlags)
	fs.BoolVar(&opts.dirForceCompression, flagPrefix+"compress", false, "Compress tarball image layers when saving to directory using the 'dir' transport. (default is same compression type as source)")
	fs.BoolVar(&opts.ociAcceptUncompressedLayers, flagPrefix+"oci-accept-uncompressed-layers", false, "Allow uncompressed image layers when saving to an OCI image using the 'oci' transport. (default is to compress things that aren't compressed)")
	fs.StringVar(&opts.compressionFormat, flagPrefix+"compress-format", "", "`FORMAT` to use for the compression")
	fs.Var(newOptionalIntValue(&opts.compressionLevel), flagPrefix+"compress-level", "`LEVEL` to use for the compression")
	return fs, &opts
}

// newSystemContext returns a *types.SystemContext corresponding to opts.
// It is guaranteed to return a fresh instance, so it is safe to make additional updates to it.
func (opts *imageDestOptions) newSystemContext() (*types.SystemContext, error) {
	ctx, err := opts.imageOptions.newSystemContext()
	if err != nil {
		return nil, err
	}

	ctx.DirForceCompress = opts.dirForceCompression
	ctx.OCIAcceptUncompressedLayers = opts.ociAcceptUncompressedLayers
	if opts.compressionFormat != "" {
		cf, err := compression.AlgorithmByName(opts.compressionFormat)
		if err != nil {
			return nil, err
		}
		ctx.CompressionFormat = &cf
	}
	if opts.compressionLevel.present {
		ctx.CompressionLevel = &opts.compressionLevel.value
	}
	return ctx, err
}

func parseCreds(creds string) (string, string, error) {
	if creds == "" {
		return "", "", errors.New("credentials can't be empty")
	}
	up := strings.SplitN(creds, ":", 2)
	if len(up) == 1 {
		return up[0], "", nil
	}
	if up[0] == "" {
		return "", "", errors.New("username can't be empty")
	}
	return up[0], up[1], nil
}

func getDockerAuth(creds string) (*types.DockerAuthConfig, error) {
	username, password, err := parseCreds(creds)
	if err != nil {
		return nil, err
	}
	return &types.DockerAuthConfig{
		Username: username,
		Password: password,
	}, nil
}

// parseImageSource converts image URL-like string to an ImageSource.
// The caller must call .Close() on the returned ImageSource.
func parseImageSource(ctx context.Context, opts *imageOptions, name string) (types.ImageSource, error) {
	ref, err := alltransports.ParseImageName(name)
	if err != nil {
		return nil, err
	}
	sys, err := opts.newSystemContext()
	if err != nil {
		return nil, err
	}
	return ref.NewImageSource(ctx, sys)
}

// parseManifestFormat parses format parameter for copy and sync command.
// It returns string value to use as manifest MIME type
func parseManifestFormat(manifestFormat string) (string, error) {
	switch manifestFormat {
	case "oci":
		return imgspecv1.MediaTypeImageManifest, nil
	case "v2s1":
		return manifest.DockerV2Schema1SignedMediaType, nil
	case "v2s2":
		return manifest.DockerV2Schema2MediaType, nil
	default:
		return "", fmt.Errorf("unknown format %q. Choose one of the supported formats: 'oci', 'v2s1', or 'v2s2'", manifestFormat)
	}
}

// usageTemplate returns the usage template for skopeo commands
// This blocks the displaying of the global options. The main skopeo
// command should not use this.
const usageTemplate = `Usage:{{if .Runnable}}
{{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}

{{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
{{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

Available Commands:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
{{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}
{{end}}
`

// adjustUsage uses usageTemplate template to get rid the GlobalOption from usage
// and disable [flag] at the end of command usage
func adjustUsage(c *cobra.Command) {
	c.SetUsageTemplate(usageTemplate)
	c.DisableFlagsInUseLine = true
}
