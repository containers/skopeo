package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/containers/common/pkg/retry"
	"github.com/containers/image/v5/docker"
	"github.com/containers/image/v5/docker/archive"
	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/transports/alltransports"
	"github.com/containers/image/v5/types"
	"github.com/spf13/cobra"
	"golang.org/x/exp/maps"
)

// tagListOutput is the output format of (skopeo list-tags), primarily so that we can format it with a simple json.MarshalIndent.
type tagListOutput struct {
	Repository string `json:",omitempty"`
	Tags       []string
}

type tagsOptions struct {
	global    *globalOptions
	image     *imageOptions
	retryOpts *retry.Options
}

var transportHandlers = map[string]func(ctx context.Context, sys *types.SystemContext, opts *tagsOptions, userInput string) (repositoryName string, tagListing []string, err error){
	docker.Transport.Name():  listDockerRepoTags,
	archive.Transport.Name(): listDockerArchiveTags,
}

// supportedTransports returns all the supported transports
func supportedTransports(joinStr string) string {
	res := maps.Keys(transportHandlers)
	sort.Strings(res)
	return strings.Join(res, joinStr)
}

func tagsCmd(global *globalOptions) *cobra.Command {
	sharedFlags, sharedOpts := sharedImageFlags()
	imageFlags, imageOpts := dockerImageFlags(global, sharedOpts, nil, "", "")
	retryFlags, retryOpts := retryFlags()

	opts := tagsOptions{
		global:    global,
		image:     imageOpts,
		retryOpts: retryOpts,
	}

	cmd := &cobra.Command{
		Use:   "list-tags [command options] SOURCE-IMAGE",
		Short: "List tags in the transport/repository specified by the SOURCE-IMAGE",
		Long: `Return the list of tags from the transport/repository "SOURCE-IMAGE"

Supported transports:
` + supportedTransports(" ") + `

See skopeo-list-tags(1) section "REPOSITORY NAMES" for the expected format
`,
		RunE:    commandAction(opts.run),
		Example: `skopeo list-tags docker://docker.io/fedora`,
	}
	adjustUsage(cmd)
	flags := cmd.Flags()
	flags.AddFlagSet(&sharedFlags)
	flags.AddFlagSet(&imageFlags)
	flags.AddFlagSet(&retryFlags)
	return cmd
}

// Customized version of the alltransports.ParseImageName and docker.ParseReference that does not place a default tag in the reference
// Would really love to not have this, but needed to enforce tag-less and digest-less names
func parseDockerRepositoryReference(refString string) (types.ImageReference, error) {
	dockerRefString, ok := strings.CutPrefix(refString, docker.Transport.Name()+"://")
	if !ok {
		return nil, fmt.Errorf("docker: image reference %s does not start with %s://", refString, docker.Transport.Name())
	}

	ref, err := reference.ParseNormalizedNamed(dockerRefString)
	if err != nil {
		return nil, err
	}

	if !reference.IsNameOnly(ref) {
		return nil, errors.New(`No tag or digest allowed in reference`)
	}

	// Checks ok, now return a reference. This is a hack because the tag listing code expects a full image reference even though the tag is ignored
	return docker.NewReference(reference.TagNameOnly(ref))
}

// List the tags from a repository contained in the imgRef reference. Any tag value in the reference is ignored
func listDockerTags(ctx context.Context, sys *types.SystemContext, imgRef types.ImageReference) (string, []string, error) {
	repositoryName := imgRef.DockerReference().Name()

	tags, err := docker.GetRepositoryTags(ctx, sys, imgRef)
	if err != nil {
		return ``, nil, fmt.Errorf("Error listing repository tags: %w", err)
	}
	return repositoryName, tags, nil
}

// return the tagLists from a docker repo
func listDockerRepoTags(ctx context.Context, sys *types.SystemContext, opts *tagsOptions, userInput string) (repositoryName string, tagListing []string, err error) {
	// Do transport-specific parsing and validation to get an image reference
	imgRef, err := parseDockerRepositoryReference(userInput)
	if err != nil {
		return
	}
	if err = retry.IfNecessary(ctx, func() error {
		repositoryName, tagListing, err = listDockerTags(ctx, sys, imgRef)
		return err
	}, opts.retryOpts); err != nil {
		return
	}
	return
}

// return the tagLists from a docker archive file
func listDockerArchiveTags(_ context.Context, sys *types.SystemContext, _ *tagsOptions, userInput string) (repositoryName string, tagListing []string, err error) {
	ref, err := alltransports.ParseImageName(userInput)
	if err != nil {
		return
	}

	tarReader, _, err := archive.NewReaderForReference(sys, ref)
	if err != nil {
		return
	}
	defer tarReader.Close()

	imageRefs, err := tarReader.List()
	if err != nil {
		return
	}

	var repoTags []string
	for imageIndex, items := range imageRefs {
		for _, ref := range items {
			repoTags, err = tarReader.ManifestTagsForReference(ref)
			if err != nil {
				return
			}
			// handle for each untagged image
			if len(repoTags) == 0 {
				repoTags = []string{fmt.Sprintf("@%d", imageIndex)}
			}
			tagListing = append(tagListing, repoTags...)
		}
	}

	return
}

func (opts *tagsOptions) run(args []string, stdout io.Writer) (retErr error) {
	ctx, cancel := opts.global.commandTimeoutContext()
	defer cancel()

	if len(args) != 1 {
		return errorShouldDisplayUsage{errors.New("Exactly one non-option argument expected")}
	}

	sys, err := opts.image.newSystemContext()
	if err != nil {
		return err
	}

	transport := alltransports.TransportFromImageName(args[0])
	if transport == nil {
		return fmt.Errorf("Invalid %q: does not specify a transport", args[0])
	}

	var repositoryName string
	var tagListing []string

	if val, ok := transportHandlers[transport.Name()]; ok {
		repositoryName, tagListing, err = val(ctx, sys, opts, args[0])
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("Unsupported transport '%s' for tag listing. Only supported: %s",
			transport.Name(), supportedTransports(", "))
	}

	outputData := tagListOutput{
		Repository: repositoryName,
		Tags:       tagListing,
	}

	out, err := json.MarshalIndent(outputData, "", "    ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s\n", string(out))

	return err
}
