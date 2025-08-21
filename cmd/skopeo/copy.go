package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	encconfig "github.com/containers/ocicrypt/config"
	enchelpers "github.com/containers/ocicrypt/helpers"
	"github.com/spf13/cobra"
	commonFlag "go.podman.io/common/pkg/flag"
	"go.podman.io/common/pkg/retry"
	"go.podman.io/image/v5/copy"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/manifest"
	"go.podman.io/image/v5/transports"
	"go.podman.io/image/v5/transports/alltransports"
)

type copyOptions struct {
	global              *globalOptions
	deprecatedTLSVerify *deprecatedTLSVerifyOption
	srcImage            *imageOptions
	destImage           *imageDestOptions
	retryOpts           *retry.Options
	copy                *sharedCopyOptions
	additionalTags      []string                  // For docker-archive: destinations, in addition to the name:tag specified as destination, also add these
	signIdentity        string                    // Identity of the signed image, must be a fully specified docker reference
	digestFile          string                    // Write digest to this file
	quiet               bool                      // Suppress output information when copying images
	all                 bool                      // Copy all of the images if the source is a list
	multiArch           commonFlag.OptionalString // How to handle multi architecture images
	encryptLayer        []int                     // The list of layers to encrypt
	encryptionKeys      []string                  // Keys needed to encrypt the image
	decryptionKeys      []string                  // Keys needed to decrypt the image
	imageParallelCopies uint                      // Maximum number of parallel requests when copying images
}

func copyCmd(global *globalOptions) *cobra.Command {
	sharedFlags, sharedOpts := sharedImageFlags()
	deprecatedTLSVerifyFlags, deprecatedTLSVerifyOpt := deprecatedTLSVerifyFlags()
	srcFlags, srcOpts := imageFlags(global, sharedOpts, deprecatedTLSVerifyOpt, "src-", "screds")
	destFlags, destOpts := imageDestFlags(global, sharedOpts, deprecatedTLSVerifyOpt, "dest-", "dcreds")
	retryFlags, retryOpts := retryFlags()
	copyFlags, copyOpts := sharedCopyFlags()
	opts := copyOptions{global: global,
		deprecatedTLSVerify: deprecatedTLSVerifyOpt,
		srcImage:            srcOpts,
		destImage:           destOpts,
		retryOpts:           retryOpts,
		copy:                copyOpts,
	}
	cmd := &cobra.Command{
		Use:   "copy [command options] SOURCE-IMAGE DESTINATION-IMAGE",
		Short: "Copy an IMAGE-NAME from one location to another",
		Long: fmt.Sprintf(`Container "IMAGE-NAME" uses a "transport":"details" format.

Supported transports:
%s

See skopeo(1) section "IMAGE NAMES" for the expected format
`, strings.Join(transports.ListNames(), ", ")),
		RunE:              commandAction(opts.run),
		Example:           `skopeo copy docker://quay.io/skopeo/stable:latest docker://registry.example.com/skopeo:latest`,
		ValidArgsFunction: autocompleteImageNames,
	}
	adjustUsage(cmd)
	flags := cmd.Flags()
	flags.AddFlagSet(&sharedFlags)
	flags.AddFlagSet(&deprecatedTLSVerifyFlags)
	flags.AddFlagSet(&srcFlags)
	flags.AddFlagSet(&destFlags)
	flags.AddFlagSet(&retryFlags)
	flags.AddFlagSet(&copyFlags)
	flags.StringSliceVar(&opts.additionalTags, "additional-tag", []string{}, "additional tags (supports docker-archive)")
	flags.BoolVarP(&opts.quiet, "quiet", "q", false, "Suppress output information when copying images")
	flags.BoolVarP(&opts.all, "all", "a", false, "Copy all images if SOURCE-IMAGE is a list")
	flags.Var(commonFlag.NewOptionalStringValue(&opts.multiArch), "multi-arch", `How to handle multi-architecture images (system, all, or index-only)`)
	flags.StringVar(&opts.signIdentity, "sign-identity", "", "Identity of signed image, must be a fully specified docker reference. Defaults to the target docker reference.")
	flags.StringVar(&opts.digestFile, "digestfile", "", "Write the digest of the pushed image to the specified file")
	flags.StringSliceVar(&opts.encryptionKeys, "encryption-key", []string{}, "*Experimental* key with the encryption protocol to use needed to encrypt the image (e.g. jwe:/path/to/key.pem)")
	flags.IntSliceVar(&opts.encryptLayer, "encrypt-layer", []int{}, "*Experimental* the 0-indexed layer indices, with support for negative indexing (e.g. 0 is the first layer, -1 is the last layer)")
	flags.StringSliceVar(&opts.decryptionKeys, "decryption-key", []string{}, "*Experimental* key needed to decrypt the image")
	flags.UintVar(&opts.imageParallelCopies, "image-parallel-copies", 0, "Maximum number of image layers to be copied (pulled/pushed) simultaneously. Not setting this field will fall back to containers/image defaults.")
	return cmd
}

// parseMultiArch parses the list processing selection
// It returns the copy.ImageListSelection to use with image.Copy option
func parseMultiArch(multiArch string) (copy.ImageListSelection, error) {
	switch multiArch {
	case "system":
		return copy.CopySystemImage, nil
	case "all":
		return copy.CopyAllImages, nil
	// There is no CopyNoImages value in copy.ImageListSelection, but because we
	// don't provide an option to select a set of images to copy, we can use
	// CopySpecificImages.
	case "index-only":
		return copy.CopySpecificImages, nil
	// We don't expose CopySpecificImages other than index-only above, because
	// we currently don't provide an option to choose the images to copy. That
	// could be added in the future.
	default:
		return copy.CopySystemImage, fmt.Errorf("unknown multi-arch option %q. Choose one of the supported options: 'system', 'all', or 'index-only'", multiArch)
	}
}

func (opts *copyOptions) run(args []string, stdout io.Writer) (retErr error) {
	if len(args) != 2 {
		return errorShouldDisplayUsage{errors.New("Exactly two arguments expected")}
	}
	opts.deprecatedTLSVerify.warnIfUsed([]string{"--src-tls-verify", "--dest-tls-verify"})
	imageNames := args

	if err := reexecIfNecessaryForImages(imageNames...); err != nil {
		return err
	}

	policyContext, err := opts.global.getPolicyContext()
	if err != nil {
		return fmt.Errorf("Error loading trust policy: %v", err)
	}
	defer func() {
		if err := policyContext.Destroy(); err != nil {
			retErr = noteCloseFailure(retErr, "tearing down policy context", err)
		}
	}()

	srcRef, err := alltransports.ParseImageName(imageNames[0])
	if err != nil {
		return fmt.Errorf("Invalid source name %s: %v", imageNames[0], err)
	}
	destRef, err := alltransports.ParseImageName(imageNames[1])
	if err != nil {
		return fmt.Errorf("Invalid destination name %s: %v", imageNames[1], err)
	}

	sourceCtx, err := opts.srcImage.newSystemContext()
	if err != nil {
		return err
	}
	destinationCtx, err := opts.destImage.newSystemContext()
	if err != nil {
		return err
	}

	for _, image := range opts.additionalTags {
		ref, err := reference.ParseNormalizedNamed(image)
		if err != nil {
			return fmt.Errorf("error parsing additional-tag '%s': %v", image, err)
		}
		namedTagged, isNamedTagged := ref.(reference.NamedTagged)
		if !isNamedTagged {
			return fmt.Errorf("additional-tag '%s' must be a tagged reference", image)
		}
		destinationCtx.DockerArchiveAdditionalTags = append(destinationCtx.DockerArchiveAdditionalTags, namedTagged)
	}

	ctx, cancel := opts.global.commandTimeoutContext()
	defer cancel()

	if opts.quiet {
		stdout = nil
	}

	imageListSelection := copy.CopySystemImage
	if opts.multiArch.Present() && opts.all {
		return fmt.Errorf("Cannot use --all and --multi-arch flags together")
	}
	if opts.multiArch.Present() {
		imageListSelection, err = parseMultiArch(opts.multiArch.Value())
		if err != nil {
			return err
		}
	}
	if opts.all {
		imageListSelection = copy.CopyAllImages
	}

	if len(opts.encryptionKeys) > 0 && len(opts.decryptionKeys) > 0 {
		return fmt.Errorf("--encryption-key and --decryption-key cannot be specified together")
	}

	var encLayers *[]int
	var encConfig *encconfig.EncryptConfig
	var decConfig *encconfig.DecryptConfig

	if len(opts.encryptLayer) > 0 && len(opts.encryptionKeys) == 0 {
		return fmt.Errorf("--encrypt-layer can only be used with --encryption-key")
	}

	if len(opts.encryptionKeys) > 0 {
		// encryption
		p := opts.encryptLayer
		encLayers = &p
		encryptionKeys := opts.encryptionKeys
		ecc, err := enchelpers.CreateCryptoConfig(encryptionKeys, []string{})
		if err != nil {
			return fmt.Errorf("Invalid encryption keys: %v", err)
		}
		cc := encconfig.CombineCryptoConfigs([]encconfig.CryptoConfig{ecc})
		encConfig = cc.EncryptConfig
	}

	if len(opts.decryptionKeys) > 0 {
		// decryption
		decryptionKeys := opts.decryptionKeys
		dcc, err := enchelpers.CreateCryptoConfig([]string{}, decryptionKeys)
		if err != nil {
			return fmt.Errorf("Invalid decryption keys: %v", err)
		}
		cc := encconfig.CombineCryptoConfigs([]encconfig.CryptoConfig{dcc})
		decConfig = cc.DecryptConfig
	}

	var signIdentity reference.Named = nil
	if opts.signIdentity != "" {
		signIdentity, err = reference.ParseNamed(opts.signIdentity)
		if err != nil {
			return fmt.Errorf("Could not parse --sign-identity: %v", err)
		}
	}

	opts.destImage.warnAboutIneffectiveOptions(destRef.Transport())

	copyOpts, cleanupOptions, err := opts.copy.copyOptions(stdout)
	if err != nil {
		return err
	}
	defer cleanupOptions()
	copyOpts.SignIdentity = signIdentity
	copyOpts.SourceCtx = sourceCtx
	copyOpts.DestinationCtx = destinationCtx
	copyOpts.ImageListSelection = imageListSelection
	copyOpts.OciDecryptConfig = decConfig
	copyOpts.OciEncryptLayers = encLayers
	copyOpts.OciEncryptConfig = encConfig
	copyOpts.MaxParallelDownloads = opts.imageParallelCopies
	copyOpts.ForceCompressionFormat = opts.destImage.forceCompressionFormat

	return retry.IfNecessary(ctx, func() error {
		manifestBytes, err := copy.Image(ctx, policyContext, destRef, srcRef, copyOpts)
		if err != nil {
			return err
		}
		if opts.digestFile != "" {
			manifestDigest, err := manifest.Digest(manifestBytes)
			if err != nil {
				return err
			}
			if err = os.WriteFile(opts.digestFile, []byte(manifestDigest.String()), 0644); err != nil {
				return fmt.Errorf("Failed to write digest to file %q: %w", opts.digestFile, err)
			}
		}
		return nil
	}, opts.retryOpts)
}
