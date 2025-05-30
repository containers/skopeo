package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	commonFlag "github.com/containers/common/pkg/flag"
	"github.com/containers/common/pkg/retry"
	"github.com/containers/image/v5/directory"
	"github.com/containers/image/v5/manifest"
	ocilayout "github.com/containers/image/v5/oci/layout"
	"github.com/containers/image/v5/pkg/compression"
	"github.com/containers/image/v5/storage"
	"github.com/containers/image/v5/transports/alltransports"
	"github.com/containers/image/v5/types"
	dockerdistributionerrcode "github.com/docker/distribution/registry/api/errcode"
	dockerdistributionapi "github.com/docker/distribution/registry/api/v2"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

// errorShouldDisplayUsage is a subtype of error used by command handlers to indicate that the command’s help should be included.
type errorShouldDisplayUsage struct {
	error
}

// noteCloseFailure returns (possibly-nil) err modified to account for (non-nil) closeErr.
// The error for closeErr is annotated with description (which is not a format string)
// Typical usage:
//
//	defer func() {
//		if err := something.Close(); err != nil {
//			returnedErr = noteCloseFailure(returnedErr, "closing something", err)
//		}
//	}
func noteCloseFailure(err error, description string, closeErr error) error {
	// We don’t accept a Closer() and close it ourselves because signature.PolicyContext has .Destroy(), not .Close().
	// This also makes it harder for a caller to do
	//     defer noteCloseFailure(returnedErr, …)
	// which doesn’t use the right value of returnedErr, and doesn’t update it.
	if err == nil {
		return fmt.Errorf("%s: %w", description, closeErr)
	}
	// In this case we prioritize the primary error for use with %w; closeErr is usually less relevant, or might be a consequence of the primary error.
	return fmt.Errorf("%w (%s: %v)", err, description, closeErr)
}

// commandAction intermediates between the RunE interface and the real handler,
// primarily to ensure that cobra.Command is not available to the handler, which in turn
// makes sure that the cmd.Flags() etc. flag access functions are not used,
// and everything is done using the *Options structures and the *Var() methods of cmd.Flag().
// handler may return errorShouldDisplayUsage to cause c.Help to be called.
func commandAction(handler func(args []string, stdout io.Writer) error) func(cmd *cobra.Command, args []string) error {
	return func(c *cobra.Command, args []string) error {
		err := handler(args, c.OutOrStdout())
		var shouldDisplayUsage errorShouldDisplayUsage
		if errors.As(err, &shouldDisplayUsage) {
			c.SetOut(c.ErrOrStderr()) // This mutates c, but we are failing anyway.
			_ = c.Help()              // Even if this failed, we prefer to report the original error
		}
		return err
	}
}

// deprecatedTLSVerifyOption represents a deprecated --tls-verify option,
// which was accepted for all subcommands, for a time.
// Every user should call deprecatedTLSVerifyOption.warnIfUsed() as part of handling the CLI,
// whether or not the value actually ends up being used.
// DO NOT ADD ANY NEW USES OF THIS; just call dockerImageFlags with an appropriate, possibly empty, flagPrefix.
type deprecatedTLSVerifyOption struct {
	tlsVerify commonFlag.OptionalBool // FIXME FIXME: Warn if this is used, or even if it is ignored.
}

// warnIfUsed warns if tlsVerify was set by the user, and suggests alternatives (which should
// start with "--").
// Every user should call this as part of handling the CLI, whether or not the value actually
// ends up being used.
func (opts *deprecatedTLSVerifyOption) warnIfUsed(alternatives []string) {
	if opts.tlsVerify.Present() {
		logrus.Warnf("'--tls-verify' is deprecated, instead use: %s", strings.Join(alternatives, ", "))
	}
}

// deprecatedTLSVerifyFlags prepares the CLI flag writing into deprecatedTLSVerifyOption, and the managed deprecatedTLSVerifyOption structure.
// DO NOT ADD ANY NEW USES OF THIS; just call dockerImageFlags with an appropriate, possibly empty, flagPrefix.
func deprecatedTLSVerifyFlags() (pflag.FlagSet, *deprecatedTLSVerifyOption) {
	opts := deprecatedTLSVerifyOption{}
	fs := pflag.FlagSet{}
	flag := commonFlag.OptionalBoolFlag(&fs, &opts.tlsVerify, "tls-verify", "require HTTPS and verify certificates when accessing the container registry")
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
	fs.StringVar(&opts.authFilePath, "authfile", os.Getenv("REGISTRY_AUTH_FILE"), "path of the registry credentials file. Default is ${XDG_RUNTIME_DIR}/containers/auth.json")
	return fs, &opts
}

// dockerImageOptions collects CLI flags specific to the "docker" transport, which are
// the same across subcommands, but may be different for each image
// (e.g. may differ between the source and destination of a copy)
type dockerImageOptions struct {
	global              *globalOptions             // May be shared across several imageOptions instances.
	shared              *sharedImageOptions        // May be shared across several imageOptions instances.
	deprecatedTLSVerify *deprecatedTLSVerifyOption // May be shared across several imageOptions instances, or nil.
	authFilePath        commonFlag.OptionalString  // Path to a */containers/auth.json (prefixed version to override shared image option).
	credsOption         commonFlag.OptionalString  // username[:password] for accessing a registry
	userName            commonFlag.OptionalString  // username for accessing a registry
	password            commonFlag.OptionalString  // password for accessing a registry
	registryToken       commonFlag.OptionalString  // token to be used directly as a Bearer token when accessing the registry
	dockerCertPath      string                     // A directory using Docker-like *.{crt,cert,key} files for connecting to a registry or a daemon
	tlsVerify           commonFlag.OptionalBool    // Require HTTPS and verify certificates (for docker: and docker-daemon:)
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
		fs.Var(commonFlag.NewOptionalStringValue(&flags.authFilePath), flagPrefix+"authfile", "path of the registry credentials file. Default is ${XDG_RUNTIME_DIR}/containers/auth.json")
	}
	fs.Var(commonFlag.NewOptionalStringValue(&flags.credsOption), flagPrefix+"creds", "Use `USERNAME[:PASSWORD]` for accessing the registry")
	fs.Var(commonFlag.NewOptionalStringValue(&flags.userName), flagPrefix+"username", "Username for accessing the registry")
	fs.Var(commonFlag.NewOptionalStringValue(&flags.password), flagPrefix+"password", "Password for accessing the registry")
	if credsOptionAlias != "" {
		// This is horribly ugly, but we need to support the old option forms of (skopeo copy) for compatibility.
		// Don't add any more cases like this.
		f := fs.VarPF(commonFlag.NewOptionalStringValue(&flags.credsOption), credsOptionAlias, "", "Use `USERNAME[:PASSWORD]` for accessing the registry")
		f.Hidden = true
	}
	fs.Var(commonFlag.NewOptionalStringValue(&flags.registryToken), flagPrefix+"registry-token", "Provide a Bearer token for accessing the registry")
	fs.StringVar(&flags.dockerCertPath, flagPrefix+"cert-dir", "", "use certificates at `PATH` (*.crt, *.cert, *.key) to connect to the registry or daemon")
	commonFlag.OptionalBoolFlag(&fs, &flags.tlsVerify, flagPrefix+"tls-verify", "require HTTPS and verify certificates when talking to the container registry or daemon")
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

func retryFlags() (pflag.FlagSet, *retry.Options) {
	opts := retry.Options{}
	fs := pflag.FlagSet{}
	fs.IntVar(&opts.MaxRetry, "retry-times", 0, "the number of times to possibly retry")
	fs.DurationVar(&opts.Delay, "retry-delay", 0*time.Second, "Fixed delay between retries. If not set, retry uses an exponential backoff delay.")
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
	if opts.authFilePath.Present() {
		ctx.AuthFilePath = opts.authFilePath.Value()
	}
	if opts.deprecatedTLSVerify != nil && opts.deprecatedTLSVerify.tlsVerify.Present() {
		// If both this deprecated option and a non-deprecated option is present, we use the latter value.
		ctx.DockerInsecureSkipTLSVerify = types.NewOptionalBool(!opts.deprecatedTLSVerify.tlsVerify.Value())
	}
	if opts.tlsVerify.Present() {
		ctx.DockerDaemonInsecureSkipTLSVerify = !opts.tlsVerify.Value()
	}
	if opts.tlsVerify.Present() {
		ctx.DockerInsecureSkipTLSVerify = types.NewOptionalBool(!opts.tlsVerify.Value())
	}
	if opts.credsOption.Present() && opts.noCreds {
		return nil, errors.New("creds and no-creds cannot be specified at the same time")
	}
	if opts.userName.Present() && opts.noCreds {
		return nil, errors.New("username and no-creds cannot be specified at the same time")
	}
	if opts.credsOption.Present() && opts.userName.Present() {
		return nil, errors.New("creds and username cannot be specified at the same time")
	}
	// if any of username or password is present, then both are expected to be present
	if opts.userName.Present() != opts.password.Present() {
		if opts.userName.Present() {
			return nil, errors.New("password must be specified when username is specified")
		}
		return nil, errors.New("username must be specified when password is specified")
	}
	if opts.credsOption.Present() {
		var err error
		ctx.DockerAuthConfig, err = getDockerAuth(opts.credsOption.Value())
		if err != nil {
			return nil, err
		}
	} else if opts.userName.Present() {
		ctx.DockerAuthConfig = &types.DockerAuthConfig{
			Username: opts.userName.Value(),
			Password: opts.password.Value(),
		}
	}
	if opts.registryToken.Present() {
		ctx.DockerBearerRegistryToken = opts.registryToken.Value()
	}
	if opts.noCreds {
		ctx.DockerAuthConfig = &types.DockerAuthConfig{}
	}

	return ctx, nil
}

// imageDestOptions is a superset of imageOptions specialized for image destinations.
// Every user should call imageDestOptions.warnAboutIneffectiveOptions() as part of handling the CLI
type imageDestOptions struct {
	*imageOptions
	dirForceCompression         bool                   // Compress layers when saving to the dir: transport
	dirForceDecompression       bool                   // Decompress layers when saving to the dir: transport
	ociAcceptUncompressedLayers bool                   // Whether to accept uncompressed layers in the oci: transport
	compressionFormat           string                 // Format to use for the compression
	compressionLevel            commonFlag.OptionalInt // Level to use for the compression
	precomputeDigests           bool                   // Precompute digests to dedup layers when saving to the docker: transport
	imageDestFlagPrefix         string
}

// imageDestFlags prepares a collection of CLI flags writing into imageDestOptions, and the managed imageDestOptions structure.
func imageDestFlags(global *globalOptions, shared *sharedImageOptions, deprecatedTLSVerify *deprecatedTLSVerifyOption, flagPrefix, credsOptionAlias string) (pflag.FlagSet, *imageDestOptions) {
	genericFlags, genericOptions := imageFlags(global, shared, deprecatedTLSVerify, flagPrefix, credsOptionAlias)
	opts := imageDestOptions{imageOptions: genericOptions, imageDestFlagPrefix: flagPrefix}
	fs := pflag.FlagSet{}
	fs.AddFlagSet(&genericFlags)
	fs.BoolVar(&opts.dirForceCompression, flagPrefix+"compress", false, "Compress tarball image layers when saving to directory using the 'dir' transport. (default is same compression type as source)")
	fs.BoolVar(&opts.dirForceDecompression, flagPrefix+"decompress", false, "Decompress tarball image layers when saving to directory using the 'dir' transport. (default is same compression type as source)")
	fs.BoolVar(&opts.ociAcceptUncompressedLayers, flagPrefix+"oci-accept-uncompressed-layers", false, "Allow uncompressed image layers when saving to an OCI image using the 'oci' transport. (default is to compress things that aren't compressed)")
	fs.StringVar(&opts.compressionFormat, flagPrefix+"compress-format", "", "`FORMAT` to use for the compression")
	fs.Var(commonFlag.NewOptionalIntValue(&opts.compressionLevel), flagPrefix+"compress-level", "`LEVEL` to use for the compression")
	fs.BoolVar(&opts.precomputeDigests, flagPrefix+"precompute-digests", false, "Precompute digests to prevent uploading layers already on the registry using the 'docker' transport.")
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
	ctx.DirForceDecompress = opts.dirForceDecompression
	ctx.OCIAcceptUncompressedLayers = opts.ociAcceptUncompressedLayers
	if opts.compressionFormat != "" {
		cf, err := compression.AlgorithmByName(opts.compressionFormat)
		if err != nil {
			return nil, err
		}
		ctx.CompressionFormat = &cf
	}
	if opts.compressionLevel.Present() {
		value := opts.compressionLevel.Value()
		ctx.CompressionLevel = &value
	}
	ctx.DockerRegistryPushPrecomputeDigests = opts.precomputeDigests
	return ctx, err
}

// warnAboutIneffectiveOptions warns if any ineffective option was set by the user
// Every user should call this as part of handling the CLI
func (opts *imageDestOptions) warnAboutIneffectiveOptions(destTransport types.ImageTransport) {
	if destTransport.Name() != directory.Transport.Name() {
		if opts.dirForceCompression {
			logrus.Warnf("--%s can only be used if the destination transport is 'dir'", opts.imageDestFlagPrefix+"compress")
		}
		if opts.dirForceDecompression {
			logrus.Warnf("--%s can only be used if the destination transport is 'dir'", opts.imageDestFlagPrefix+"decompress")
		}
	}
}

func parseCreds(creds string) (string, string, error) {
	if creds == "" {
		return "", "", errors.New("credentials can't be empty")
	}
	username, password, _ := strings.Cut(creds, ":") // Sets password to "" if there is no ":"
	if username == "" {
		return "", "", errors.New("username can't be empty")
	}
	return username, password, nil
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

// promptForPassphrase interactively prompts for a passphrase related to privateKeyFile
func promptForPassphrase(privateKeyFile string, stdin, stdout *os.File) (string, error) {
	stdinFd := int(stdin.Fd())
	if !term.IsTerminal(stdinFd) {
		return "", fmt.Errorf("Cannot prompt for a passphrase for key %s, standard input is not a TTY", privateKeyFile)
	}

	fmt.Fprintf(stdout, "Passphrase for key %s: ", privateKeyFile)
	passphrase, err := term.ReadPassword(stdinFd)
	if err != nil {
		return "", fmt.Errorf("Error reading password: %w", err)
	}
	fmt.Fprintf(stdout, "\n")
	return string(passphrase), nil
}

// isNotFoundImageError heuristically attempts to determine whether an error
// is saying the remote source couldn't find the image (as opposed to an
// authentication error, an I/O error etc.)
// TODO drive this into containers/image properly
func isNotFoundImageError(err error) bool {
	return isDockerManifestUnknownError(err) ||
		errors.Is(err, storage.ErrNoSuchImage) ||
		errors.Is(err, ocilayout.ImageNotFoundError{})
}

// isDockerManifestUnknownError is a copy of code from containers/image,
// please update there first.
func isDockerManifestUnknownError(err error) bool {
	var ec dockerdistributionerrcode.ErrorCoder
	if !errors.As(err, &ec) {
		return false
	}
	return ec.ErrorCode() == dockerdistributionapi.ErrorCodeManifestUnknown
}
