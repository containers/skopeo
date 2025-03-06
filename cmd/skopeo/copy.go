package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	commonFlag "github.com/containers/common/pkg/flag"
	"github.com/containers/common/pkg/retry"
	"github.com/containers/image/v5/copy"
	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/pkg/cli"
	"github.com/containers/image/v5/pkg/cli/sigstore"
	"github.com/containers/image/v5/signature/signer"
	"github.com/containers/image/v5/transports"
	"github.com/containers/image/v5/transports/alltransports"
	encconfig "github.com/containers/ocicrypt/config"
	enchelpers "github.com/containers/ocicrypt/helpers"
	"github.com/spf13/cobra"
)

type copyOptions struct {
	global                   *globalOptions
	deprecatedTLSVerify      *deprecatedTLSVerifyOption
	srcImage                 *imageOptions
	destImage                *imageDestOptions
	retryOpts                *retry.Options
	additionalTags           []string                  // For docker-archive: destinations, in addition to the name:tag specified as destination, also add these
	removeSignatures         bool                      // Do not copy signatures from the source image
	signByFingerprint        string                    // Sign the image using a GPG key with the specified fingerprint
	signBySigstoreParamFile  string                    // Sign the image using a sigstore signature per configuration in a param file
	signBySigstorePrivateKey string                    // Sign the image using a sigstore private key
	signPassphraseFile       string                    // Path pointing to a passphrase file when signing (for either signature format, but only one of them)
	signIdentity             string                    // Identity of the signed image, must be a fully specified docker reference
	digestFile               string                    // Write digest to this file
	format                   commonFlag.OptionalString // Force conversion of the image to a specified format
	quiet                    bool                      // Suppress output information when copying images
	all                      bool                      // Copy all of the images if the source is a list
	multiArch                commonFlag.OptionalString // How to handle multi architecture images
	preserveDigests          bool                      // Preserve digests during copy
	encryptLayer             []int                     // The list of layers to encrypt
	encryptionKeys           []string                  // Keys needed to encrypt the image
	decryptionKeys           []string                  // Keys needed to decrypt the image
	imageParallelCopies      uint                      // Maximum number of parallel requests when copying images
}

func copyCmd(global *globalOptions) *cobra.Command {
	sharedFlags, sharedOpts := sharedImageFlags()
	deprecatedTLSVerifyFlags, deprecatedTLSVerifyOpt := deprecatedTLSVerifyFlags()
	srcFlags, srcOpts := imageFlags(global, sharedOpts, deprecatedTLSVerifyOpt, "src-", "screds")
	destFlags, destOpts := imageDestFlags(global, sharedOpts, deprecatedTLSVerifyOpt, "dest-", "dcreds")
	retryFlags, retryOpts := retryFlags()
	opts := copyOptions{global: global,
		deprecatedTLSVerify: deprecatedTLSVerifyOpt,
		srcImage:            srcOpts,
		destImage:           destOpts,
		retryOpts:           retryOpts,
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
	flags.StringSliceVar(&opts.additionalTags, "additional-tag", []string{}, "additional tags (supports docker-archive)")
	flags.BoolVarP(&opts.quiet, "quiet", "q", false, "Suppress output information when copying images")
	flags.BoolVarP(&opts.all, "all", "a", false, "Copy all images if SOURCE-IMAGE is a list")
	flags.Var(commonFlag.NewOptionalStringValue(&opts.multiArch), "multi-arch", `How to handle multi-architecture images (system, all, or index-only)`)
	flags.BoolVar(&opts.preserveDigests, "preserve-digests", false, "Preserve digests of images and lists")
	flags.BoolVar(&opts.removeSignatures, "remove-signatures", false, "Do not copy signatures from SOURCE-IMAGE")
	flags.StringVar(&opts.signByFingerprint, "sign-by", "", "Sign the image using a GPG key with the specified `FINGERPRINT`")
	flags.StringVar(&opts.signBySigstoreParamFile, "sign-by-sigstore", "", "Sign the image using a sigstore parameter file at `PATH`")
	flags.StringVar(&opts.signBySigstorePrivateKey, "sign-by-sigstore-private-key", "", "Sign the image using a sigstore private key at `PATH`")
	flags.StringVar(&opts.signPassphraseFile, "sign-passphrase-file", "", "Read a passphrase for signing an image from `PATH`")
	flags.StringVar(&opts.signIdentity, "sign-identity", "", "Identity of signed image, must be a fully specified docker reference. Defaults to the target docker reference.")
	flags.StringVar(&opts.digestFile, "digestfile", "", "Write the digest of the pushed image to the specified file")
	flags.VarP(commonFlag.NewOptionalStringValue(&opts.format), "format", "f", `MANIFEST TYPE (oci, v2s1, or v2s2) to use in the destination (default is manifest type of source, with fallbacks)`)
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

	var manifestType string
	if opts.format.Present() {
		manifestType, err = parseManifestFormat(opts.format.Value())
		if err != nil {
			return err
		}
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

	// c/image/copy.Image does allow creating both simple signing and sigstore signatures simultaneously,
	// with independent passphrases, but that would make the CLI probably too confusing.
	// For now, use the passphrase with either, but only one of them.
	if opts.signPassphraseFile != "" && opts.signByFingerprint != "" && opts.signBySigstorePrivateKey != "" {
		return fmt.Errorf("Only one of --sign-by and sign-by-sigstore-private-key can be used with sign-passphrase-file")
	}
	var passphrase string
	if opts.signPassphraseFile != "" {
		p, err := cli.ReadPassphraseFile(opts.signPassphraseFile)
		if err != nil {
			return err
		}
		passphrase = p
	} else if opts.signBySigstorePrivateKey != "" {
		p, err := promptForPassphrase(opts.signBySigstorePrivateKey, os.Stdin, os.Stdout)
		if err != nil {
			return err
		}
		passphrase = p
	} // opts.signByFingerprint triggers a GPG-agent passphrase prompt, possibly using a more secure channel, so we usually shouldn’t prompt ourselves if no passphrase was explicitly provided.

	var signers []*signer.Signer
	if opts.signBySigstoreParamFile != "" {
		signer, err := sigstore.NewSignerFromParameterFile(opts.signBySigstoreParamFile, &sigstore.Options{
			PrivateKeyPassphrasePrompt: func(keyFile string) (string, error) {
				return promptForPassphrase(keyFile, os.Stdin, os.Stdout)
			},
			Stdin:  os.Stdin,
			Stdout: stdout,
		})
		if err != nil {
			return fmt.Errorf("Error using --sign-by-sigstore: %w", err)
		}
		defer signer.Close()
		signers = append(signers, signer)
	}

	var signIdentity reference.Named = nil
	if opts.signIdentity != "" {
		signIdentity, err = reference.ParseNamed(opts.signIdentity)
		if err != nil {
			return fmt.Errorf("Could not parse --sign-identity: %v", err)
		}
	}

	opts.destImage.warnAboutIneffectiveOptions(destRef.Transport())

	return retry.IfNecessary(ctx, func() error {
		manifestBytes, err := copy.Image(ctx, policyContext, destRef, srcRef, &copy.Options{
			RemoveSignatures:                 opts.removeSignatures,
			Signers:                          signers,
			SignBy:                           opts.signByFingerprint,
			SignPassphrase:                   passphrase,
			SignBySigstorePrivateKeyFile:     opts.signBySigstorePrivateKey,
			SignSigstorePrivateKeyPassphrase: []byte(passphrase),
			SignIdentity:                     signIdentity,
			ReportWriter:                     stdout,
			SourceCtx:                        sourceCtx,
			DestinationCtx:                   destinationCtx,
			ForceManifestMIMEType:            manifestType,
			ImageListSelection:               imageListSelection,
			PreserveDigests:                  opts.preserveDigests,
			OciDecryptConfig:                 decConfig,
			OciEncryptLayers:                 encLayers,
			OciEncryptConfig:                 encConfig,
			MaxParallelDownloads:             opts.imageParallelCopies,
		})
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
