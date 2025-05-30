% SKOPEO(1) Skopeo Man Pages
% Jhon Honce
% August 2016
## NAME
skopeo -- Command line utility used to interact with local and remote container images and container image registries

## SYNOPSIS
**skopeo** [_global options_] _command_ [_command options_]

## DESCRIPTION
`skopeo` is a command line utility providing various operations with container images and container image registries.

`skopeo` can copy container images between various containers image stores, converting them as necessary.  For example you can use `skopeo` to copy container images from one container registry to another.

`skopeo` can convert a Docker schema 2 or schema 1 container image to an OCI image.

`skopeo` can inspect a repository on a container registry without needlessly pulling the image. Pulling an image from a repository, especially a remote repository, is an expensive network and storage operation. Skopeo fetches the repository's manifest and displays a `docker inspect`-like json output about the repository or a tag. `skopeo`, in contrast to `docker inspect`, helps you gather useful information about a repository or a tag without requiring you to run `docker pull` - e.g. - Which tags are available for the given repository? Which labels does the image have?

`skopeo` can sign and verify container images.

`skopeo` can delete container images from a remote container registry.

Note: `skopeo` does not require any container runtimes to be running, to do most of
its functionality.  It also does not require root, unless you are copying images into a container runtime storage backend, like the docker daemon or github.com/containers/storage.

## IMAGE NAMES
Most commands refer to container images, using a _transport_`:`_details_ format. The following formats are supported:

  **containers-storage:**_docker-reference_
  An image located in a local containers/storage image store.  Both the location and image store are specified in /etc/containers/storage.conf. (Backend for Podman, CRI-O, Buildah and friends)

  **dir:**_path_
  An existing local directory _path_ storing the manifest, layer tarballs and signatures as individual files. This is a non-standardized format, primarily useful for debugging or noninvasive container inspection.

  **docker://**_docker-reference_
  An image in a registry implementing the "Docker Registry HTTP API V2".
  Credentials are typically managed using `(skopeo login)`;
  see **containers-auth.json**(5) for more details about the credential search mechanism.

  **docker-archive:**_path_[**:**_docker-reference_]
  An image is stored in the `docker save` formatted file.  _docker-reference_ is only used when creating such a file, and it must not contain a digest.

  **docker-daemon:**_docker-reference_
  An image _docker-reference_ stored in the docker daemon internal storage.  _docker-reference_ must contain either a tag or a digest.  Alternatively, when reading images, the format can be docker-daemon:algo:digest (an image ID).

  **oci:**_path_**:**_tag_
  An image _tag_ in a directory compliant with "Open Container Image Layout Specification" at _path_.

  **oci-archive:**_path_**:**_tag_
  An image _tag_ in a tar archive compliant with "Open Container Image Layout Specification" at _path_.

See [containers-transports(5)](https://github.com/containers/image/blob/main/docs/containers-transports.5.md) for details.

## OPTIONS

These options should be placed before the subcommand name.
Individual subcommands have their own options.

**--command-timeout** _duration_

Timeout for the command execution.

**--debug**

enable debug output

**--help**, **-h**

Show help

**--insecure-policy**

Adopt an insecure, permissive policy that allows anything. This obviates the need for a policy file.

**--override-arch** _arch_

Use _arch_ instead of the architecture of the machine for choosing images.

**--override-os** _os_

Use _OS_ instead of the running OS for choosing images.

**--override-variant** _variant_

Use _variant_ instead of the running architecture variant for choosing images.

**--policy** _path-to-policy_

Path to a policy.json file to use for verifying signatures and deciding whether an image is trusted, overriding the default trust policy file.

**--registries.d** _dir_

Use registry configuration files in _dir_ (e.g. for container signature storage), overriding the default path.

**--tmpdir** _dir_

Directory used to store temporary files. Defaults to /var/tmp.

**--version**, **-v**

Print the version number

## COMMANDS

| Command                                   | Description                                                                    |
| ----------------------------------------- | ------------------------------------------------------------------------------ |
| [skopeo-copy(1)](skopeo-copy.1.md)        | Copy an image (manifest, filesystem layers, signatures) from one location to another. |
| [skopeo-delete(1)](skopeo-delete.1.md)    | Mark the _image-name_ for later deletion by the registry's garbage collector.  |
| [skopeo-generate-sigstore-key(1)](skopeo-generate-sigstore-key.1.md)    | Generate a sigstore public/private key pair.  |
| [skopeo-inspect(1)](skopeo-inspect.1.md)  | Return low-level information about _image-name_ in a registry.                 |
| [skopeo-list-tags(1)](skopeo-list-tags.1.md)  | List image names in a transport-specific collection of images.|
| [skopeo-login(1)](skopeo-login.1.md)  | Login to a container registry. |
| [skopeo-logout(1)](skopeo-logout.1.md)  | Logout of a container registry. |
| [skopeo-manifest-digest(1)](skopeo-manifest-digest.1.md)    | Compute a manifest digest for a manifest-file and write it to standard output. |
| [skopeo-standalone-sign(1)](skopeo-standalone-sign.1.md)    | Debugging tool - Sign an image locally without uploading.    |
| [skopeo-standalone-verify(1)](skopeo-standalone-verify.1.md)| Debugging tool - Verify an image signature from local files. |
| [skopeo-sync(1)](skopeo-sync.1.md)| Synchronize images between registry repositories and local directories.                |

## EXIT STATUS
`skopeo` exits with status 0 on success, non-zero on error.

Details about the exit statuses:

**1** Generic error, details can be found in the error message.

**2** The input image cannot be found. Note that this is best effort and for remote registries the status often cannot be reliably reported.

## FILES
  **/etc/containers/policy.json**
  Default trust policy file, if **--policy** is not specified.
  The policy format is documented in [containers-policy.json(5)](https://github.com/containers/image/blob/main/docs/containers-policy.json.5.md) .

  **/etc/containers/registries.d**
  Default directory containing registry configuration, if **--registries.d** is not specified.
  The contents of this directory are documented in [containers-registries.d(5)](https://github.com/containers/image/blob/main/docs/containers-registries.d.5.md).

## SEE ALSO
skopeo-login(1), docker-login(1), containers-auth.json(5), containers-storage.conf(5), containers-policy.json(5), containers-transports(5)

## AUTHORS

Antonio Murdaca <runcom@redhat.com>, Miloslav Trmac <mitr@redhat.com>, Jhon Honce <jhonce@redhat.com>
