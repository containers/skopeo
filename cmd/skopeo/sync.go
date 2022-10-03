package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	commonFlag "github.com/containers/common/pkg/flag"
	"github.com/containers/common/pkg/retry"
	"github.com/containers/image/v5/copy"
	"github.com/containers/image/v5/directory"
	"github.com/containers/image/v5/docker"
	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/pkg/cli"
	"github.com/containers/image/v5/transports"
	"github.com/containers/image/v5/types"
	"github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

// syncOptions contains information retrieved from the skopeo sync command line.
type syncOptions struct {
	global                   *globalOptions // Global (not command dependent) skopeo options
	deprecatedTLSVerify      *deprecatedTLSVerifyOption
	srcImage                 *imageOptions     // Source image options
	destImage                *imageDestOptions // Destination image options
	retryOpts                *retry.Options
	removeSignatures         bool                      // Do not copy signatures from the source image
	signByFingerprint        string                    // Sign the image using a GPG key with the specified fingerprint
	signBySigstorePrivateKey string                    // Sign the image using a sigstore private key
	signPassphraseFile       string                    // Path pointing to a passphrase file when signing
	format                   commonFlag.OptionalString // Force conversion of the image to a specified format
	source                   string                    // Source repository name
	destination              string                    // Destination registry name
	scoped                   bool                      // When true, namespace copied images at destination using the source repository name
	all                      bool                      // Copy all of the images if an image in the source is a list
	dryRun                   bool                      // Don't actually copy anything, just output what it would have done
	preserveDigests          bool                      // Preserve digests during sync
	keepGoing                bool                      // Whether or not to abort the sync if there are any errors during syncing the images
}

// repoDescriptor contains information of a single repository used as a sync source.
type repoDescriptor struct {
	DirBasePath          string                 // base path when source is 'dir'
	ImageRefs            []types.ImageReference // List of tagged image found for the repository
	Context              *types.SystemContext   // SystemContext for the sync command
	DestinationPathStyle string                 // path style for the destination
}

// tlsVerifyConfig is an implementation of the Unmarshaler interface, used to
// customize the unmarshaling behaviour of the tls-verify YAML key.
type tlsVerifyConfig struct {
	skip types.OptionalBool // skip TLS verification check (false by default)
}

// registrySyncConfig contains information about a single registry, read from
// the source YAML file
type registrySyncConfig struct {
	Images               map[string][]string    // Images map images name to slices with the images' references (tags, digests)
	ImagesByTagRegex     map[string]string      `yaml:"images-by-tag-regex"` // Images map images name to regular expression with the images' tags
	Credentials          types.DockerAuthConfig // Username and password used to authenticate with the registry
	TLSVerify            tlsVerifyConfig        `yaml:"tls-verify"`             // TLS verification mode (enabled by default)
	CertDir              string                 `yaml:"cert-dir"`               // Path to the TLS certificates of the registry
	DestinationPathStyle string                 `yaml:"destination-path-style"` // path style for the destination
}

// sourceConfig contains all registries information read from the source YAML file
type sourceConfig map[string]registrySyncConfig

func syncCmd(global *globalOptions) *cobra.Command {
	sharedFlags, sharedOpts := sharedImageFlags()
	deprecatedTLSVerifyFlags, deprecatedTLSVerifyOpt := deprecatedTLSVerifyFlags()
	srcFlags, srcOpts := dockerImageFlags(global, sharedOpts, deprecatedTLSVerifyOpt, "src-", "screds")
	destFlags, destOpts := dockerImageFlags(global, sharedOpts, deprecatedTLSVerifyOpt, "dest-", "dcreds")
	retryFlags, retryOpts := retryFlags()

	opts := syncOptions{
		global:              global,
		deprecatedTLSVerify: deprecatedTLSVerifyOpt,
		srcImage:            srcOpts,
		destImage:           &imageDestOptions{imageOptions: destOpts},
		retryOpts:           retryOpts,
	}

	cmd := &cobra.Command{
		Use:   "sync [command options] --src TRANSPORT --dest TRANSPORT SOURCE DESTINATION",
		Short: "Synchronize one or more images from one location to another",
		Long: `Copy all the images from a SOURCE to a DESTINATION.

Allowed SOURCE transports (specified with --src): docker, dir, yaml.
Allowed DESTINATION transports (specified with --dest): docker, dir.

See skopeo-sync(1) for details.
`,
		RunE:    commandAction(opts.run),
		Example: `skopeo sync --src docker --dest dir --scoped registry.example.com/busybox /media/usb`,
	}
	adjustUsage(cmd)
	flags := cmd.Flags()
	flags.BoolVar(&opts.removeSignatures, "remove-signatures", false, "Do not copy signatures from SOURCE images")
	flags.StringVar(&opts.signByFingerprint, "sign-by", "", "Sign the image using a GPG key with the specified `FINGERPRINT`")
	flags.StringVar(&opts.signBySigstorePrivateKey, "sign-by-sigstore-private-key", "", "Sign the image using a sigstore private key at `PATH`")
	flags.StringVar(&opts.signPassphraseFile, "sign-passphrase-file", "", "File that contains a passphrase for the --sign-by key")
	flags.VarP(commonFlag.NewOptionalStringValue(&opts.format), "format", "f", `MANIFEST TYPE (oci, v2s1, or v2s2) to use when syncing image(s) to a destination (default is manifest type of source, with fallbacks)`)
	flags.StringVarP(&opts.source, "src", "s", "", "SOURCE transport type")
	flags.StringVarP(&opts.destination, "dest", "d", "", "DESTINATION transport type")
	flags.BoolVar(&opts.scoped, "scoped", false, "Images at DESTINATION are prefix using the full source image path as scope")
	flags.BoolVarP(&opts.all, "all", "a", false, "Copy all images if SOURCE-IMAGE is a list")
	flags.BoolVar(&opts.dryRun, "dry-run", false, "Run without actually copying data")
	flags.BoolVar(&opts.preserveDigests, "preserve-digests", false, "Preserve digests of images and lists")
	flags.BoolVarP(&opts.keepGoing, "keep-going", "", false, "Do not abort the sync if any image copy fails")
	flags.AddFlagSet(&sharedFlags)
	flags.AddFlagSet(&deprecatedTLSVerifyFlags)
	flags.AddFlagSet(&srcFlags)
	flags.AddFlagSet(&destFlags)
	flags.AddFlagSet(&retryFlags)
	return cmd
}

// UnmarshalYAML is the implementation of the Unmarshaler interface method
// method for the tlsVerifyConfig type.
// It unmarshals the 'tls-verify' YAML key so that, when they key is not
// specified, tls verification is enforced.
func (tls *tlsVerifyConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var verify bool
	if err := unmarshal(&verify); err != nil {
		return err
	}

	tls.skip = types.NewOptionalBool(!verify)
	return nil
}

// newSourceConfig unmarshals the provided YAML file path to the sourceConfig type.
// It returns a new unmarshaled sourceConfig object and any error encountered.
func newSourceConfig(yamlFile string) (sourceConfig, error) {
	var cfg sourceConfig
	source, err := os.ReadFile(yamlFile)
	if err != nil {
		return cfg, err
	}
	err = yaml.Unmarshal(source, &cfg)
	if err != nil {
		return cfg, fmt.Errorf("Failed to unmarshal %q: %w", yamlFile, err)
	}
	return cfg, nil
}

// parseRepositoryReference parses input into a reference.Named, and verifies that it names a repository, not an image.
func parseRepositoryReference(input string) (reference.Named, error) {
	ref, err := reference.ParseNormalizedNamed(input)
	if err != nil {
		return nil, err
	}
	if !reference.IsNameOnly(ref) {
		return nil, errors.New("input names a reference, not a repository")
	}
	return ref, nil
}

// destinationReference creates an image reference using the provided transport.
// It returns a image reference to be used as destination of an image copy and
// any error encountered.
func destinationReference(destination string, transport string) (types.ImageReference, error) {
	var imageTransport types.ImageTransport

	switch transport {
	case docker.Transport.Name():
		destination = fmt.Sprintf("//%s", destination)
		imageTransport = docker.Transport
	case directory.Transport.Name():
		_, err := os.Stat(destination)
		if err == nil {
			return nil, fmt.Errorf("Refusing to overwrite destination directory %q", destination)
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("Destination directory could not be used: %w", err)
		}
		// the directory holding the image must be created here
		if err = os.MkdirAll(destination, 0755); err != nil {
			return nil, fmt.Errorf("Error creating directory for image %s: %w", destination, err)
		}
		imageTransport = directory.Transport
	default:
		return nil, fmt.Errorf("%q is not a valid destination transport", transport)
	}
	logrus.Debugf("Destination for transport %q: %s", transport, destination)

	destRef, err := imageTransport.ParseReference(destination)
	if err != nil {
		return nil, fmt.Errorf("Cannot obtain a valid image reference for transport %q and reference %q: %w", imageTransport.Name(), destination, err)
	}

	return destRef, nil
}

// getImageTags lists all tags in a repository.
// It returns a string slice of tags and any error encountered.
func getImageTags(ctx context.Context, sysCtx *types.SystemContext, repoRef reference.Named) ([]string, error) {
	name := repoRef.Name()
	logrus.WithFields(logrus.Fields{
		"image": name,
	}).Info("Getting tags")
	// Ugly: NewReference rejects IsNameOnly references, and GetRepositoryTags ignores the tag/digest.
	// So, we use TagNameOnly here only to shut up NewReference
	dockerRef, err := docker.NewReference(reference.TagNameOnly(repoRef))
	if err != nil {
		return nil, err // Should never happen for a reference with tag and no digest
	}
	tags, err := docker.GetRepositoryTags(ctx, sysCtx, dockerRef)
	if err != nil {
		var unauthorizedForCredentials docker.ErrUnauthorizedForCredentials
		if errors.As(err, &unauthorizedForCredentials) {
			// Some registries may decide to block the "list all tags" endpoint.
			// Gracefully allow the sync to continue in this case.
			logrus.Warnf("Registry disallows tag list retrieval: %s", err)
			tags = nil
		} else {
			return nil, fmt.Errorf("Error determining repository tags for image %s: %w", name, err)
		}
	}

	return tags, nil
}

// imagesToCopyFromRepo builds a list of image references from the tags
// found in a source repository.
// It returns an image reference slice with as many elements as the tags found
// and any error encountered.
func imagesToCopyFromRepo(sys *types.SystemContext, repoRef reference.Named) ([]types.ImageReference, error) {
	tags, err := getImageTags(context.Background(), sys, repoRef)
	if err != nil {
		return nil, err
	}

	var sourceReferences []types.ImageReference
	for _, tag := range tags {
		taggedRef, err := reference.WithTag(repoRef, tag)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"repo": repoRef.Name(),
				"tag":  tag,
			}).Errorf("Error creating a tagged reference from registry tag list: %v", err)
			continue
		}
		ref, err := docker.NewReference(taggedRef)
		if err != nil {
			return nil, fmt.Errorf("Cannot obtain a valid image reference for transport %q and reference %s: %w", docker.Transport.Name(), taggedRef.String(), err)
		}
		sourceReferences = append(sourceReferences, ref)
	}
	return sourceReferences, nil
}

// imagesToCopyFromDir builds a list of image references from the images found
// in the source directory.
// It returns an image reference slice with as many elements as the images found
// and any error encountered.
func imagesToCopyFromDir(dirPath string) ([]types.ImageReference, error) {
	var sourceReferences []types.ImageReference
	err := filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "manifest.json" {
			dirname := filepath.Dir(path)
			ref, err := directory.Transport.ParseReference(dirname)
			if err != nil {
				return fmt.Errorf("Cannot obtain a valid image reference for transport %q and reference %q: %w", directory.Transport.Name(), dirname, err)
			}
			sourceReferences = append(sourceReferences, ref)
			return filepath.SkipDir
		}
		return nil
	})

	if err != nil {
		return sourceReferences,
			fmt.Errorf("Error walking the path %q: %w", dirPath, err)
	}

	return sourceReferences, nil
}

// imagesToCopyFromRegistry builds a list of repository descriptors from the images
// in a registry configuration.
// It returns a repository descriptors slice with as many elements as the images
// found and any error encountered. Each element of the slice is a list of
// image references, to be used as sync source.
func imagesToCopyFromRegistry(registryName string, cfg registrySyncConfig, sourceCtx types.SystemContext) ([]repoDescriptor, error) {
	serverCtx := &sourceCtx
	// override ctx with per-registryName options
	serverCtx.DockerCertPath = cfg.CertDir
	serverCtx.DockerDaemonCertPath = cfg.CertDir
	serverCtx.DockerDaemonInsecureSkipTLSVerify = (cfg.TLSVerify.skip == types.OptionalBoolTrue)
	serverCtx.DockerInsecureSkipTLSVerify = cfg.TLSVerify.skip
	if cfg.Credentials != (types.DockerAuthConfig{}) {
		serverCtx.DockerAuthConfig = &cfg.Credentials
	}
	var repoDescList []repoDescriptor
	for imageName, refs := range cfg.Images {
		repoLogger := logrus.WithFields(logrus.Fields{
			"repo":     imageName,
			"registry": registryName,
		})
		repoRef, err := parseRepositoryReference(fmt.Sprintf("%s/%s", registryName, imageName))
		if err != nil {
			repoLogger.Error("Error parsing repository name, skipping")
			logrus.Error(err)
			continue
		}

		repoLogger.Info("Processing repo")

		var sourceReferences []types.ImageReference
		if len(refs) != 0 {
			for _, ref := range refs {
				tagLogger := logrus.WithFields(logrus.Fields{"ref": ref})
				var named reference.Named
				// first try as digest
				if d, err := digest.Parse(ref); err == nil {
					named, err = reference.WithDigest(repoRef, d)
					if err != nil {
						tagLogger.Error("Error processing ref, skipping")
						logrus.Error(err)
						continue
					}
				} else {
					tagLogger.Debugf("Ref was not a digest, trying as a tag: %s", err)
					named, err = reference.WithTag(repoRef, ref)
					if err != nil {
						tagLogger.Error("Error parsing ref, skipping")
						logrus.Error(err)
						continue
					}
				}

				imageRef, err := docker.NewReference(named)
				if err != nil {
					tagLogger.Error("Error processing ref, skipping")
					logrus.Errorf("Error getting image reference: %s", err)
					continue
				}
				sourceReferences = append(sourceReferences, imageRef)
			}
		} else { // len(refs) == 0
			repoLogger.Info("Querying registry for image tags")
			sourceReferences, err = imagesToCopyFromRepo(serverCtx, repoRef)
			if err != nil {
				repoLogger.Error("Error processing repo, skipping")
				logrus.Error(err)
				continue
			}
		}

		if len(sourceReferences) == 0 {
			repoLogger.Warnf("No refs to sync found")
			continue
		}
		repoDescList = append(repoDescList, repoDescriptor{
			ImageRefs:            sourceReferences,
			Context:              serverCtx,
			DestinationPathStyle: cfg.DestinationPathStyle})
	}

	for imageName, tagRegex := range cfg.ImagesByTagRegex {
		repoLogger := logrus.WithFields(logrus.Fields{
			"repo":     imageName,
			"registry": registryName,
		})
		repoRef, err := parseRepositoryReference(fmt.Sprintf("%s/%s", registryName, imageName))
		if err != nil {
			repoLogger.Error("Error parsing repository name, skipping")
			logrus.Error(err)
			continue
		}

		repoLogger.Info("Processing repo")

		var sourceReferences []types.ImageReference

		tagReg, err := regexp.Compile(tagRegex)
		if err != nil {
			repoLogger.WithFields(logrus.Fields{
				"regex": tagRegex,
			}).Error("Error parsing regex, skipping")
			logrus.Error(err)
			continue
		}

		repoLogger.Info("Querying registry for image tags")
		allSourceReferences, err := imagesToCopyFromRepo(serverCtx, repoRef)
		if err != nil {
			repoLogger.Error("Error processing repo, skipping")
			logrus.Error(err)
			continue
		}

		repoLogger.Infof("Start filtering using the regular expression: %v", tagRegex)
		for _, sReference := range allSourceReferences {
			tagged, isTagged := sReference.DockerReference().(reference.Tagged)
			if !isTagged {
				repoLogger.Errorf("Internal error, reference %s does not have a tag, skipping", sReference.DockerReference())
				continue
			}
			if tagReg.MatchString(tagged.Tag()) {
				sourceReferences = append(sourceReferences, sReference)
			}
		}

		if len(sourceReferences) == 0 {
			repoLogger.Warnf("No refs to sync found")
			continue
		}
		repoDescList = append(repoDescList, repoDescriptor{
			ImageRefs:            sourceReferences,
			Context:              serverCtx,
			DestinationPathStyle: cfg.DestinationPathStyle})
	}

	return repoDescList, nil
}

// imagesToCopy retrieves all the images to copy from a specified sync source
// and transport.
// It returns a slice of repository descriptors, where each descriptor is a
// list of tagged image references to be used as sync source, and any error
// encountered.
func imagesToCopy(source string, transport string, sourceCtx *types.SystemContext) ([]repoDescriptor, error) {
	var descriptors []repoDescriptor

	switch transport {
	case docker.Transport.Name():
		desc := repoDescriptor{
			Context: sourceCtx,
		}
		named, err := reference.ParseNormalizedNamed(source) // May be a repository or an image.
		if err != nil {
			return nil, fmt.Errorf("Cannot obtain a valid image reference for transport %q and reference %q: %w", docker.Transport.Name(), source, err)
		}
		imageTagged := !reference.IsNameOnly(named)
		logrus.WithFields(logrus.Fields{
			"imagename": source,
			"tagged":    imageTagged,
		}).Info("Tag presence check")
		if imageTagged {
			srcRef, err := docker.NewReference(named)
			if err != nil {
				return nil, fmt.Errorf("Cannot obtain a valid image reference for transport %q and reference %q: %w", docker.Transport.Name(), named.String(), err)
			}
			desc.ImageRefs = []types.ImageReference{srcRef}
		} else {
			desc.ImageRefs, err = imagesToCopyFromRepo(sourceCtx, named)
			if err != nil {
				return descriptors, err
			}
			if len(desc.ImageRefs) == 0 {
				return descriptors, fmt.Errorf("No images to sync found in %q", source)
			}
		}
		descriptors = append(descriptors, desc)

	case directory.Transport.Name():
		desc := repoDescriptor{
			Context: sourceCtx,
		}

		if _, err := os.Stat(source); err != nil {
			return descriptors, fmt.Errorf("Invalid source directory specified: %w", err)
		}
		desc.DirBasePath = source
		var err error
		desc.ImageRefs, err = imagesToCopyFromDir(source)
		if err != nil {
			return descriptors, err
		}
		if len(desc.ImageRefs) == 0 {
			return descriptors, fmt.Errorf("No images to sync found in %q", source)
		}
		descriptors = append(descriptors, desc)

	case "yaml":
		cfg, err := newSourceConfig(source)
		if err != nil {
			return descriptors, err
		}
		for registryName, registryConfig := range cfg {
			if len(registryConfig.Images) == 0 && len(registryConfig.ImagesByTagRegex) == 0 {
				logrus.WithFields(logrus.Fields{
					"registry": registryName,
				}).Warn("No images specified for registry")
				continue
			}

			descs, err := imagesToCopyFromRegistry(registryName, registryConfig, *sourceCtx)
			if err != nil {
				return descriptors, fmt.Errorf("Failed to retrieve list of images from registry %q: %w", registryName, err)
			}
			descriptors = append(descriptors, descs...)
		}
	}

	return descriptors, nil
}

func (opts *syncOptions) run(args []string, stdout io.Writer) (retErr error) {
	if len(args) != 2 {
		return errorShouldDisplayUsage{errors.New("Exactly two arguments expected")}
	}
	opts.deprecatedTLSVerify.warnIfUsed([]string{"--src-tls-verify", "--dest-tls-verify"})

	policyContext, err := opts.global.getPolicyContext()
	if err != nil {
		return fmt.Errorf("Error loading trust policy: %w", err)
	}
	defer func() {
		if err := policyContext.Destroy(); err != nil {
			retErr = noteCloseFailure(retErr, "tearing down policy context", err)
		}
	}()

	// validate source and destination options
	contains := func(val string, list []string) (_ bool) {
		for _, l := range list {
			if l == val {
				return true
			}
		}
		return
	}

	if len(opts.source) == 0 {
		return errors.New("A source transport must be specified")
	}
	if !contains(opts.source, []string{docker.Transport.Name(), directory.Transport.Name(), "yaml"}) {
		return fmt.Errorf("%q is not a valid source transport", opts.source)
	}

	if len(opts.destination) == 0 {
		return errors.New("A destination transport must be specified")
	}
	if !contains(opts.destination, []string{docker.Transport.Name(), directory.Transport.Name()}) {
		return fmt.Errorf("%q is not a valid destination transport", opts.destination)
	}

	if opts.source == opts.destination && opts.source == directory.Transport.Name() {
		return errors.New("sync from 'dir' to 'dir' not implemented, consider using rsync instead")
	}

	opts.destImage.warnAboutIneffectiveOptions(transports.Get(opts.destination))

	imageListSelection := copy.CopySystemImage
	if opts.all {
		imageListSelection = copy.CopyAllImages
	}

	sourceCtx, err := opts.srcImage.newSystemContext()
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

	ctx, cancel := opts.global.commandTimeoutContext()
	defer cancel()

	sourceArg := args[0]
	var srcRepoList []repoDescriptor
	if err = retry.IfNecessary(ctx, func() error {
		srcRepoList, err = imagesToCopy(sourceArg, opts.source, sourceCtx)
		return err
	}, opts.retryOpts); err != nil {
		return err
	}

	destination := args[1]
	destinationCtx, err := opts.destImage.newSystemContext()
	if err != nil {
		return err
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
	}
	options := copy.Options{
		RemoveSignatures:                      opts.removeSignatures,
		SignBy:                                opts.signByFingerprint,
		SignPassphrase:                        passphrase,
		SignBySigstorePrivateKeyFile:          opts.signBySigstorePrivateKey,
		SignSigstorePrivateKeyPassphrase:      []byte(passphrase),
		ReportWriter:                          os.Stdout,
		DestinationCtx:                        destinationCtx,
		ImageListSelection:                    imageListSelection,
		PreserveDigests:                       opts.preserveDigests,
		OptimizeDestinationImageAlreadyExists: true,
		ForceManifestMIMEType:                 manifestType,
	}
	errorsPresent := false
	imagesNumber := 0
	if opts.dryRun {
		logrus.Warn("Running in dry-run mode")
	}

	for _, srcRepo := range srcRepoList {
		options.SourceCtx = srcRepo.Context
		for counter, ref := range srcRepo.ImageRefs {
			var destSuffix string
			switch ref.Transport() {
			case docker.Transport:
				// docker -> dir or docker -> docker
				destSuffix = ref.DockerReference().String()
			case directory.Transport:
				// dir -> docker (we don't allow `dir` -> `dir` sync operations)
				destSuffix = strings.TrimPrefix(ref.StringWithinTransport(), srcRepo.DirBasePath)
				if destSuffix == "" {
					// if source is a full path to an image, have destPath scoped to repo:tag
					destSuffix = path.Base(srcRepo.DirBasePath)
				}
			}

			var destSuffixStyle string
			destSuffixStyle = srcRepo.DestinationPathStyle

			if opts.scoped {
				if !(destSuffixStyle == "full" || destSuffixStyle == "") {
					logrus.Infof("destination-style-path setting in yaml is overridden by global --scoped cli flag")
				}
				destSuffixStyle = "full"
			}

			switch destSuffixStyle {
			case "flat":
				destSuffix = path.Base(destSuffix)
			case "full":
			case "full-no-registry":
				destSuffix = strings.SplitN(destSuffix, "/", 2)[1]
			default:
				if opts.source == "yaml" && destSuffixStyle != "" {
					logrus.Warnf("destination-path-style yaml setting doesn't match 'full', 'full-no-registry' or 'flat', but is '%s'. Using 'flat' style as default.", destSuffixStyle)
				}
				destSuffix = path.Base(destSuffix)
			}

			destRef, err := destinationReference(path.Join(destination, destSuffix), opts.destination)
			if err != nil {
				return err
			}

			fromToFields := logrus.Fields{
				"from": transports.ImageName(ref),
				"to":   transports.ImageName(destRef),
			}
			if opts.dryRun {
				logrus.WithFields(fromToFields).Infof("Would have copied image ref %d/%d", counter+1, len(srcRepo.ImageRefs))
			} else {
				logrus.WithFields(fromToFields).Infof("Copying image ref %d/%d", counter+1, len(srcRepo.ImageRefs))
				if err = retry.IfNecessary(ctx, func() error {
					_, err = copy.Image(ctx, policyContext, destRef, ref, &options)
					return err
				}, opts.retryOpts); err != nil {
					if !opts.keepGoing {
						return fmt.Errorf("Error copying ref %q: %w", transports.ImageName(ref), err)
					}
					// log the error, keep a note that there was a failure and move on to the next
					// image ref
					errorsPresent = true
					logrus.WithError(err).Errorf("Error copying ref %q", transports.ImageName(ref))
					continue
				}
			}
			imagesNumber++
		}
	}

	if opts.dryRun {
		logrus.Infof("Would have synced %d images from %d sources", imagesNumber, len(srcRepoList))
	} else {
		logrus.Infof("Synced %d images from %d sources", imagesNumber, len(srcRepoList))
	}
	if !errorsPresent {
		return nil
	}
	return errors.New("Sync failed due to previous reported error(s) for one or more images")
}
