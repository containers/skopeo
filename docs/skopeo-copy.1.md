% skopeo-copy(1)

## NAME
skopeo\-copy - Copy an image (manifest, filesystem layers, signatures) from one location to another.

## SYNOPSIS
**skopeo copy** [*options*] _source-image_ _destination-image_

## DESCRIPTION
Copy an image (manifest, filesystem layers, signatures) from one location to another.

Uses the system's trust policy to validate images, rejects images not trusted by the policy.

  _source-image_ use the "image name" format described above

  _destination-image_ use the "image name" format described above

_source-image_ and _destination-image_ are interpreted completely independently; e.g. the destination name does not
automatically inherit any parts of the source name.

## OPTIONS

See also [skopeo(1)](skopeo.1.md) for options placed before the subcommand name.

**--additional-tag**=_strings_

Additional tags (supports docker-archive).

**--all**, **-a**

If _source-image_ refers to a list of images, instead of copying just the image which matches the current OS and
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

**--dest-shared-blob-dir** _directory_

Directory to use to share blobs across OCI repositories.

**--digestfile** _path_

After copying the image, write the digest of the resulting image to the file.

**--preserve-digests**

Preserve the digests during copying. Fail if the digest cannot be preserved.

This option does not change what will be copied; consider using `--all` at the same time.

**--encrypt-layer** _ints_

*Experimental* the 0-indexed layer indices, with support for negative indexing (e.g. 0 is the first layer, -1 is the last layer)

**--format**, **-f** _manifest-type_

MANIFEST TYPE (oci, v2s1, or v2s2) to use in the destination (default is manifest type of source, with fallbacks)

**--help**, **-h**

Print usage statement

**--multi-arch** _option_

Control what is copied if _source-image_ refers to a multi-architecture image. Default is system.

Options:
- system: Copy only the image that matches the system architecture
- all: Copy the full multi-architecture image
- index-only: Copy only the index

The index-only option usually fails unless the referenced per-architecture images are already present in the destination, or the target registry supports sparse indexes.

**--quiet**, **-q**

Suppress output information when copying images.

**--remove-signatures**

Do not copy signatures, if any, from _source-image_. Necessary when copying a signed image to a destination which does not support signatures.

**--sign-by** _key-id_

Add a “simple signing” signature using that key ID for an image name corresponding to _destination-image_

**--sign-by-sigstore** _param-file_

Add a sigstore signature based on the options in the specified containers sigstore signing parameter file, _param-file_.
See containers-sigstore-signing-params.yaml(5) for details about the file format.

**--sign-by-sigstore-private-key** _path_

Add a sigstore signature using a private key at _path_ for an image name corresponding to _destination-image_

**--sign-passphrase-file** _path_

The passphare to use when signing with `--sign-by` or `--sign-by-sigstore-private-key`. Only the first line will be read. A passphrase stored in a file is of questionable security if other users can read this file. Do not use this option if at all avoidable.

**--sign-identity** _reference_

The identity to use when signing the image. The identity must be a fully specified docker reference. If the identity is not specified, the target docker reference will be used.

**--src-shared-blob-dir** _directory_

Directory to use to share blobs across OCI repositories.

**--encryption-key** _protocol:keyfile_

Specifies the encryption protocol, which can be JWE (RFC7516), PGP (RFC4880), and PKCS7 (RFC2315) and the key material required for image encryption. For instance, jwe:/path/to/key.pem or pgp:admin@example.com or pkcs7:/path/to/x509-file.

**--decryption-key** _key[:passphrase]_

Key to be used for decryption of images. Key can point to keys and/or certificates. Decryption will be tried with all keys. If the key is protected by a passphrase, it is required to be passed in the argument and omitted otherwise.

**--src-creds** _username[:password]_

Credentials for accessing the source registry.

**--dest-compress**

Compress tarball image layers when saving to directory using the 'dir' transport. (default is same compression type as source).

**--dest-decompress**

Decompress tarball image layers when saving to directory using the 'dir' transport. (default is same compression type as source).

**--dest-oci-accept-uncompressed-layers**

Allow uncompressed image layers when saving to an OCI image using the 'oci' transport. (default is to compress things that aren't compressed).

**--dest-creds** _username[:password]_

Credentials for accessing the destination registry.

**--src-cert-dir** _path_

Use certificates at _path_ (*.crt, *.cert, *.key) to connect to the source registry or daemon.

**--src-no-creds**

Access the registry anonymously.

**--src-tls-verify**=_bool_

Require HTTPS and verify certificates when talking to container source registry or daemon. Default to source registry setting.

**--dest-cert-dir** _path_

Use certificates at _path_ (*.crt, *.cert, *.key) to connect to the destination registry or daemon.

**--dest-no-creds**

Access the registry anonymously.

**--dest-tls-verify**=_bool_

Require HTTPS and verify certificates when talking to container destination registry or daemon. Default to destination registry setting.

**--src-daemon-host** _host_

Copy from docker daemon at _host_. If _host_ starts with `tcp://`, HTTPS is enabled by default. To use plain HTTP, use the form `http://` (default is `unix:///var/run/docker.sock`).

**--dest-daemon-host** _host_

Copy to docker daemon at _host_. If _host_ starts with `tcp://`, HTTPS is enabled by default. To use plain HTTP, use the form `http://` (default is `unix:///var/run/docker.sock`).

Existing signatures, if any, are preserved as well.

**--dest-compress-format** _format_

Specifies the compression format to use.  Supported values are: `gzip`, `zstd` and `zstd:chunked`.
`zstd:chunked` is incompatible with encrypting images,
and will be treated as `zstd` with a warning in that case.

**--dest-compress-level** _format_

Specifies the compression level to use.  The value is specific to the compression algorithm used, e.g. for zstd the accepted values are in the range 1-20 (inclusive), while for gzip it is 1-9 (inclusive).

**--src-registry-token** _token_

Bearer token for accessing the source registry.

**--dest-registry-token** _token_

Bearer token for accessing the destination registry.

**--dest-precompute-digests**

Precompute digests to ensure layers are not uploaded that already exist on the destination registry. Layers with initially unknown digests (ex. compressing "on the fly") will be temporarily streamed to disk.

**--retry-times**

The number of times to retry.

**--retry-delay**

Fixed delay between retries. If not set (or set to 0s), retry wait time will be exponentially increased based on the number of failed attempts.

**--src-username**

The username to access the source registry.

**--src-password**

The password to access the source registry.

**--dest-username**

The username to access the destination registry.

**--dest-password**

The password to access the destination registry.

**--image-parallel-copies** _n_

Maximum number of image layers to be copied (pulled/pushed) simultaneously. Not setting this field will fall back to containers/image defaults.

## EXAMPLES

To just copy an image from one registry to another:
```console
$ skopeo copy docker://quay.io/skopeo/stable:latest docker://registry.example.com/skopeo:latest
```

To copy the layers of the docker.io busybox image to a local directory:
```console
$ mkdir -p /var/lib/images/busybox
$ skopeo copy docker://busybox:latest dir:/var/lib/images/busybox
$ ls /var/lib/images/busybox/*
  /tmp/busybox/2b8fd9751c4c0f5dd266fcae00707e67a2545ef34f9a29354585f93dac906749.tar
  /tmp/busybox/manifest.json
  /tmp/busybox/8ddc19f16526912237dd8af81971d5e4dd0587907234be2b83e249518d5b673f.tar
```

To create an archive consumable by `docker load` (but note that using a registry is almost always more efficient):
```console
$ skopeo copy docker://busybox:latest docker-archive:archive-file.tar:busybox:latest
```

To copy and sign an image:
```console
$ skopeo copy --sign-by dev@example.com containers-storage:example/busybox:streaming docker://example/busybox:gold
```

To encrypt an image:
```console
$ skopeo copy docker://docker.io/library/nginx:1.17.8 oci:local_nginx:1.17.8

$ openssl genrsa -out private.key 1024
$ openssl rsa -in private.key -pubout > public.key

$ skopeo copy --encryption-key jwe:./public.key oci:local_nginx:1.17.8 oci:try-encrypt:encrypted
```

To decrypt an image:
```console
$ skopeo copy --decryption-key ./private.key oci:try-encrypt:encrypted oci:try-decrypt:decrypted
```

To copy encrypted image without decryption:
```console
$ skopeo copy oci:try-encrypt:encrypted oci:try-encrypt-copy:encrypted
```

To decrypt an image that requires more than one key:
```console
$ skopeo copy --decryption-key ./private1.key --decryption-key ./private2.key --decryption-key ./private3.key oci:try-encrypt:encrypted oci:try-decrypt:decrypted
```

Container images can also be partially encrypted by specifying the index of the layer. Layers are 0-indexed indices, with support for negative indexing. i.e. 0 is the first layer, -1 is the last layer.

Let's say out of 3 layers that the image `docker.io/library/nginx:1.17.8` is made up of, we only want to encrypt the 2nd layer,
```console
$ skopeo copy --encryption-key jwe:./public.key --encrypt-layer 1 oci:local_nginx:1.17.8 oci:try-encrypt:encrypted
```

## SEE ALSO
skopeo(1), skopeo-login(1), docker-login(1), containers-auth.json(5), containers-policy.json(5), containers-transports(5), containers-signature(5)

## AUTHORS

Antonio Murdaca <runcom@redhat.com>, Miloslav Trmac <mitr@redhat.com>, Jhon Honce <jhonce@redhat.com>
