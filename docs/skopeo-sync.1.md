% skopeo-sync(1)

## NAME
skopeo\-sync - Synchronize images between registry repositories and local directories.


## SYNOPSIS
**skopeo sync** [*options*] --src _transport_ --dest _transport_ _source_ _destination_

## DESCRIPTION
Synchronize images between registry repositories and local directories. Synchronization is achieved by copying all the images found at _source_ to _destination_ - useful when synchronizing a local container registry mirror or for populating registries running inside of air-gapped environments.

Differently from other skopeo commands, skopeo sync requires both source and destination transports to be specified separately from _source_ and _destination_.
One of the problems of prefixing a destination with its transport is that, the registry `docker://hostname:port` would be wrongly interpreted as an image reference at a non-fully qualified registry, with `hostname` and `port` the image name and tag.

Available _source_ transports:
 - _docker_ (i.e. `--src docker`): _source_ is a repository hosted on a container registry (e.g.: `registry.example.com/busybox`).
 If no image tag is specified, skopeo sync copies all the tags found in that repository.
 - _dir_ (i.e. `--src dir`): _source_ is a local directory path (e.g.: `/media/usb/`). Refer to skopeo(1) **dir:**_path_ for the local image format.
 - _yaml_ (i.e. `--src yaml`): _source_ is local YAML file path.
 The YAML file should specify the list of images copied from different container registries (local directories are not supported). Refer to EXAMPLES for the file format.

Available _destination_ transports:
 - _docker_ (i.e. `--dest docker`): _destination_ is a container registry (e.g.: `my-registry.local.lan`).
 - _dir_ (i.e. `--dest dir`): _destination_ is a local directory path (e.g.: `/media/usb/`).
 One directory per source 'image:tag' is created for each copied image.

When the `--scoped` option is specified, images are prefixed with the source image path so that multiple images with the same
name can be stored at _destination_.

## OPTIONS

See also [skopeo(1)](skopeo.1.md) for options placed before the subcommand name.

**--all**, **-a**
If one of the images in __src__ refers to a list of images, instead of copying just the image which matches the current OS and
architecture (subject to the use of the global --override-os, --override-arch and --override-variant options), attempt to copy all of
the images in the list, and the list itself.

**--authfile** _path_

Path of the primary registry credentials file. On Linux, the default is ${XDG\_RUNTIME\_DIR}/containers/auth.json.
See **containers-auth.json**(5) for more details about the credential search mechanism and defaults on other platforms.

Use `skopeo login` to manage the credentials.

The default value of this option is read from the `REGISTRY\_AUTH\_FILE` environment variable.

**--src-authfile** _path_

Path of the primary registry credentials file for the source registry. Uses path given by `--authfile`, if not provided.

**--dest-authfile** _path_

Path of the primary registry credentials file for the destination registry. Uses path given by `--authfile`, if not provided.

**--dry-run**

Run the sync without actually copying data to the destination.

**--src**, **-s** _transport_ Transport for the source repository.

**--dest**, **-d** _transport_ Destination transport.

**--format**, **-f** _manifest-type_ Manifest Type (oci, v2s1, or v2s2) to use when syncing image(s) to a destination (default is manifest type of source, with fallbacks).

**--help**, **-h**

Print usage statement.

**--scoped** Prefix images with the source image path, so that multiple images with the same name can be stored at _destination_.

**--append-suffix** _tag-suffix_ String to append to destination tags.

**--digestfile** _path_

After copying the images from source, write the digest of the resulting images along with Image Reference.

```
sha256:bf91f90823248017a4f920fb541727fa8368dc6cf377a7debbd271cf6a31c8a7 docker://myhost.com/alpine:edge
sha256:31603596830fc7e56753139f9c2c6bd3759e48a850659506ebfb885d1cf3aef5 docker://myhost.com/postgres:14.3

```

**--preserve-digests**

Preserve the digests during copying. Fail if the digest cannot be preserved.

This option does not change what will be copied; consider using `--all` at the same time.

**--remove-signatures** Do not copy signatures, if any, from _source-image_. This is necessary when copying a signed image to a destination which does not support signatures.

**--sign-by** _key-id_

Add a “simple signing” signature using that key ID for an image name corresponding to _destination-image_

**--sign-by-sigstore** _param-file_

Add a sigstore signature based on the options in the specified containers sigstore signing parameter file, _param-file_.
See containers-sigstore-signing-params.yaml(5) for details about the file format.

**--sign-by-sigstore-private-key** _path_

Add a sigstore signature using a private key at _path_ for an image name corresponding to _destination-image_

**--sign-passphrase-file** _path_

The passphare to use when signing with `--sign-by` or `--sign-by-sigstore-private-key`. Only the first line will be read. A passphrase stored in a file is of questionable security if other users can read this file. Do not use this option if at all avoidable.

**--src-creds** _username[:password]_ for accessing the source registry.

**--dest-creds** _username[:password]_ for accessing the destination registry.

**--src-cert-dir** _path_ Use certificates (*.crt, *.cert, *.key) at _path_ to connect to the source registry or daemon.

**--src-no-creds** Access the registry anonymously.

**--src-tls-verify**=_bool_ Require HTTPS and verify certificates when talking to a container source registry or daemon. Default to source registry entry in registry.conf setting.

**--dest-cert-dir** _path_ Use certificates (*.crt, *.cert, *.key) at _path_ to connect to the destination registry or daemon.

**--dest-no-creds** Access the registry anonymously.

**--dest-tls-verify**=_bool_ Require HTTPS and verify certificates when talking to a container destination registry or daemon. Default to destination registry entry in registry.conf setting.

**--src-registry-token** _Bearer token_ for accessing the source registry.

**--dest-registry-token** _Bearer token_ for accessing the destination registry.

**--retry-times**

The number of times to retry.

**--retry-delay**

Fixed delay between retries. If not set (or set to 0s), retry wait time will be exponentially increased based on the number of failed attempts.

**--keep-going**
If any errors occur during copying of images, those errors are logged and the process continues syncing rest of the images and finally fails at the end.

**--src-username**

The username to access the source registry.

**--src-password**

The password to access the source registry.

**--dest-username**

The username to access the destination registry.

**--dest-password**

The password to access the destination registry.

## EXAMPLES

### Synchronizing to a local directory
```console
$ skopeo sync --src docker --dest dir registry.example.com/busybox /media/usb
```
Images are located at:
```
/media/usb/busybox:1-glibc
/media/usb/busybox:1-musl
/media/usb/busybox:1-ubuntu
...
/media/usb/busybox:latest
```

### Synchronizing to a container registry from local
Images are located at:
```
/media/usb/busybox:1-glibc
```
Sync run
```console
$ skopeo sync --src dir --dest docker /media/usb/busybox:1-glibc my-registry.local.lan/test/
```
Destination registry content:
```
REPO                                 TAGS
my-registry.local.lan/test/busybox   1-glibc
```

### Synchronizing to a local directory, scoped
```console
$ skopeo sync --src docker --dest dir --scoped registry.example.com/busybox /media/usb
```
Images are located at:
```
/media/usb/registry.example.com/busybox:1-glibc
/media/usb/registry.example.com/busybox:1-musl
/media/usb/registry.example.com/busybox:1-ubuntu
...
/media/usb/registry.example.com/busybox:latest
```

### Synchronizing to a container registry
```console
$ skopeo sync --src docker --dest docker registry.example.com/busybox my-registry.local.lan
```
Destination registry content:
```
REPO                         TAGS
registry.local.lan/busybox   1-glibc, 1-musl, 1-ubuntu, ..., latest
```

### Synchronizing to a container registry keeping the repository
```console
$ skopeo sync --src docker --dest docker registry.example.com/repo/busybox my-registry.local.lan/repo
```
Destination registry content:
```
REPO                              TAGS
registry.local.lan/repo/busybox   1-glibc, 1-musl, 1-ubuntu, ..., latest
```

### Synchronizing to a container registry with tag suffix
```console
$ skopeo sync --src docker --dest docker --append-suffix '-mirror' registry.example.com/busybox my-registry.local.lan
```
Destination registry content:
```
REPO                         TAGS
registry.local.lan/busybox   1-glibc-mirror, 1-musl-mirror, 1-ubuntu-mirror, ..., latest-mirror
```

### YAML file content (used _source_ for `**--src yaml**`)

```yaml
registry.example.com:
    images:
        busybox: []
        redis:
            - "1.0"
            - "2.0"
            - "sha256:0000000000000000000000000000000011111111111111111111111111111111"
    images-by-tag-regex:
        nginx: ^1\.13\.[12]-alpine-perl$
    images-by-semver:
        alpine: ">= 3.12.0"
    credentials:
        username: john
        password: this is a secret
    tls-verify: true
    cert-dir: /home/john/certs
quay.io:
    tls-verify: false
    images:
        coreos/etcd:
            - latest
```
If the yaml filename is `sync.yml`, sync run:
```console
$ skopeo sync --src yaml --dest docker sync.yml my-registry.local.lan/repo/
```
This will copy the following images:
- Repository `registry.example.com/busybox`: all images, as no tags are specified.
- Repository `registry.example.com/redis`: images tagged "1.0" and "2.0" along with image with digest "sha256:0000000000000000000000000000000011111111111111111111111111111111".
- Repository `registry.example.com/nginx`: images tagged "1.13.1-alpine-perl" and "1.13.2-alpine-perl".
- Repository `quay.io/coreos/etcd`: images tagged "latest".
- Repository `registry.example.com/alpine`: all images with tags match the semantic version constraint ">= 3.12.0" ("3.12.0, "3.12.1", ... ,"4.0.0", ...)

The full list of possible semantic version comparisons can be found in the
upstream library's documentation:
https://github.com/Masterminds/semver/tree/v3.2.0#basic-comparisons.

Version ordering and precedence is understood as defined here:
https://semver.org/#spec-item-11.

For the registry `registry.example.com`, the "john"/"this is a secret" credentials are used, with server TLS certificates located at `/home/john/certs`.

TLS verification is normally enabled, and it can be disabled setting `tls-verify` to `false`.
In the above example, TLS verification is enabled for `registry.example.com`, while is
disabled for `quay.io`.

## SEE ALSO
skopeo(1), skopeo-login(1), docker-login(1), containers-auth.json(5), containers-policy.json(5), containers-transports(5)

## AUTHORS

Flavio Castelli <fcastelli@suse.com>, Marco Vedovati <mvedovati@suse.com>
