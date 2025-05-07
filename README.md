<p align="center">
   <img src="https://cdn.rawgit.com/containers/skopeo/main/docs/skopeo.svg" width="250" alt="Skopeo">
</p>

----
![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)
![GitHub release (latest SemVer)](https://img.shields.io/github/v/release/containers/skopeo)
[![Go Report Card](https://goreportcard.com/badge/github.com/containers/skopeo)](https://goreportcard.com/report/github.com/containers/skopeo)

`skopeo` is a command line utility that performs various operations on container images and image repositories.

`skopeo` does not require the user to be running as root to do most of its operations.

`skopeo` does not require a daemon to be running to perform its operations.

`skopeo` can work with [OCI images](https://github.com/opencontainers/image-spec) as well as the original Docker v2 images.

Skopeo works with API V2 container image registries such as [docker.io](https://docker.io) and [quay.io](https://quay.io) registries, private registries, local directories and local OCI-layout directories. Skopeo can perform operations which consist of:

 * Copying an image from and to various storage mechanisms.
   For example you can copy images from one registry to another, without requiring privilege.
 * Inspecting a remote image showing its properties including its layers, without requiring you to pull the image to the host.
 * Deleting an image from an image repository.
 * Syncing an external image repository to an internal registry for air-gapped deployments.
 * When required by the repository, skopeo can pass the appropriate credentials and certificates for authentication.

 Skopeo operates on the following image and repository types:

 * containers-storage:docker-reference
         An image located in a local containers/storage image store.  Both the location and image store are specified in /etc/containers/storage.conf. (This is  the backend for [Podman](https://podman.io), [CRI-O](https://cri-o.io), [Buildah](https://buildah.io) and friends)

 * dir:path
         An existing local directory path storing the manifest, layer tarballs and signatures as individual files. This is a non-standardized format, primarily useful for debugging or noninvasive container inspection.

 * docker://docker-reference
         An image in a registry implementing the "Docker Registry HTTP API V2". By default, uses the authorization state in `$XDG_RUNTIME_DIR/containers/auth.json`, which is set using `skopeo login`.

 * docker-archive:path[:docker-reference]
         An image is stored in a `docker save`-formatted file.  docker-reference is only used when creating such a file, and it must not contain a digest.

 * docker-daemon:docker-reference
         An image docker-reference stored in the docker daemon internal storage.  docker-reference must contain either a tag or a digest.  Alternatively, when reading images, the format can also be docker-daemon:algo:digest (an image ID).

 * oci:path:tag
         An image tag in a directory compliant with "Open Container Image Layout Specification" at path.

[Obtaining skopeo](./install.md)
-

For a detailed description how to install or build skopeo, see
[install.md](./install.md).

Skopeo is also available as a Container Image on [quay.io](https://quay.io/skopeo/stable).  For more information, see the [Skopeo Image](https://github.com/containers/image_build/blob/main/skopeo/README.md) page.

## Inspecting a repository
`skopeo` is able to _inspect_ a repository on a container registry and fetch images layers.
The _inspect_ command fetches the repository's manifest and it is able to show you a `docker inspect`-like
json output about a whole repository or a tag. This tool, in contrast to `docker inspect`, helps you gather useful information about
a repository or a tag before pulling it (using disk space).  The inspect command can show you which tags are available for the given 
repository, the labels the image has, the creation date and operating system of the image and more.  

Examples:

#### Show properties of fedora:latest
```console
$ skopeo inspect docker://registry.fedoraproject.org/fedora:latest
{
    "Name": "registry.fedoraproject.org/fedora",
    "Digest": "sha256:0f65bee641e821f8118acafb44c2f8fe30c2fc6b9a2b3729c0660376391aa117",
    "RepoTags": [
        "34-aarch64",
        "34",
        "latest",
        ...
    ],
    "Created": "2022-11-24T13:54:18Z",
    "DockerVersion": "1.10.1",
    "Labels": {
        "license": "MIT",
        "name": "fedora",
        "vendor": "Fedora Project",
        "version": "37"
    },
    "Architecture": "amd64",
    "Os": "linux",
    "Layers": [
        "sha256:2a0fc6bf62e155737f0ace6142ee686f3c471c1aab4241dc3128904db46288f0"
    ],
    "LayersData": [
        {
            "MIMEType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
            "Digest": "sha256:2a0fc6bf62e155737f0ace6142ee686f3c471c1aab4241dc3128904db46288f0",
            "Size": 71355009,
            "Annotations": null
        }
    ],
    "Env": [
        "DISTTAG=f37container",
        "FGC=f37",
        "container=oci"
    ]
}
```

#### Show container configuration from `fedora:latest`

```console
$ skopeo inspect --config docker://registry.fedoraproject.org/fedora:latest  | jq
{
  "created": "2020-04-29T06:48:16Z",
  "architecture": "amd64",
  "os": "linux",
  "config": {
    "Env": [
      "DISTTAG=f32container",
      "FGC=f32",
      "container=oci"
    ],
    "Cmd": [
      "/bin/bash"
    ],
    "Labels": {
      "license": "MIT",
      "name": "fedora",
      "vendor": "Fedora Project",
      "version": "32"
    }
  },
  "rootfs": {
    "type": "layers",
    "diff_ids": [
      "sha256:a4c0fa2b217d3fd63d51e55a6fd59432e543d499c0df2b1acd48fbe424f2ddd1"
    ]
  },
  "history": [
    {
      "created": "2020-04-29T06:48:16Z",
      "comment": "Created by Image Factory"
    }
  ]
}
```
#### Show unverified image's digest
```console
$ skopeo inspect docker://registry.fedoraproject.org/fedora:latest | jq '.Digest'
"sha256:655721ff613ee766a4126cb5e0d5ae81598e1b0c3bcf7017c36c4d72cb092fe9"
```

## Copying images

`skopeo` can copy container images between various storage mechanisms, including:
* Container registries

  -  The Quay, Docker Hub, OpenShift, GCR, Artifactory ...

* Container Storage backends

  -  [github.com/containers/storage](https://github.com/containers/storage) (Backend for [Podman](https://podman.io), [CRI-O](https://cri-o.io), [Buildah](https://buildah.io) and friends)

  -  Docker daemon storage

* Local directories

* Local OCI-layout directories

```console
$ skopeo copy docker://quay.io/buildah/stable docker://registry.internal.company.com/buildah
$ skopeo copy oci:busybox_ocilayout:latest dir:existingemptydirectory
```

## Deleting images
```console
$ skopeo delete docker://localhost:5000/imagename:latest
```

## Syncing registries
```console
$ skopeo sync --src docker --dest dir registry.example.com/busybox /media/usb
```

## Authenticating to a registry

#### Private registries with authentication
skopeo uses credentials from the --creds (for skopeo inspect|delete) or --src-creds|--dest-creds (for skopeo copy) flags, if set; otherwise it uses configuration set by skopeo login, podman login, buildah login, or docker login.

```console
$ skopeo login --username USER myregistrydomain.com:5000
Password:
$ skopeo inspect docker://myregistrydomain.com:5000/busybox
{"Tag":"latest","Digest":"sha256:473bb2189d7b913ed7187a33d11e743fdc2f88931122a44d91a301b64419f092","RepoTags":["latest"],"Comment":"","Created":"2016-01-15T18:06:41.282540103Z","ContainerConfig":{"Hostname":"aded96b43f48","Domainname":"","User":"","AttachStdin":false,"AttachStdout":false,"AttachStderr":false,"Tty":false,"OpenStdin":false,"StdinOnce":false,"Env":null,"Cmd":["/bin/sh","-c","#(nop) CMD [\"sh\"]"],"Image":"9e77fef7a1c9f989988c06620dabc4020c607885b959a2cbd7c2283c91da3e33","Volumes":null,"WorkingDir":"","Entrypoint":null,"OnBuild":null,"Labels":null},"DockerVersion":"1.8.3","Author":"","Config":{"Hostname":"aded96b43f48","Domainname":"","User":"","AttachStdin":false,"AttachStdout":false,"AttachStderr":false,"Tty":false,"OpenStdin":false,"StdinOnce":false,"Env":null,"Cmd":["sh"],"Image":"9e77fef7a1c9f989988c06620dabc4020c607885b959a2cbd7c2283c91da3e33","Volumes":null,"WorkingDir":"","Entrypoint":null,"OnBuild":null,"Labels":null},"Architecture":"amd64","Os":"linux"}
$ skopeo logout myregistrydomain.com:5000
```

#### Using --creds directly

```console
$ skopeo inspect --creds=testuser:testpassword docker://myregistrydomain.com:5000/busybox
{"Tag":"latest","Digest":"sha256:473bb2189d7b913ed7187a33d11e743fdc2f88931122a44d91a301b64419f092","RepoTags":["latest"],"Comment":"","Created":"2016-01-15T18:06:41.282540103Z","ContainerConfig":{"Hostname":"aded96b43f48","Domainname":"","User":"","AttachStdin":false,"AttachStdout":false,"AttachStderr":false,"Tty":false,"OpenStdin":false,"StdinOnce":false,"Env":null,"Cmd":["/bin/sh","-c","#(nop) CMD [\"sh\"]"],"Image":"9e77fef7a1c9f989988c06620dabc4020c607885b959a2cbd7c2283c91da3e33","Volumes":null,"WorkingDir":"","Entrypoint":null,"OnBuild":null,"Labels":null},"DockerVersion":"1.8.3","Author":"","Config":{"Hostname":"aded96b43f48","Domainname":"","User":"","AttachStdin":false,"AttachStdout":false,"AttachStderr":false,"Tty":false,"OpenStdin":false,"StdinOnce":false,"Env":null,"Cmd":["sh"],"Image":"9e77fef7a1c9f989988c06620dabc4020c607885b959a2cbd7c2283c91da3e33","Volumes":null,"WorkingDir":"","Entrypoint":null,"OnBuild":null,"Labels":null},"Architecture":"amd64","Os":"linux"}
```

```console
$ skopeo copy --src-creds=testuser:testpassword docker://myregistrydomain.com:5000/private oci:local_oci_image
```

Contributing
-

Please read the [contribution guide](CONTRIBUTING.md) if you want to collaborate in the project.

## Commands
| Command                                            | Description                                                                                  |
| -------------------------------------------------- | ---------------------------------------------------------------------------------------------|
| [skopeo-copy(1)](/docs/skopeo-copy.1.md)           | Copy an image (manifest, filesystem layers, signatures) from one location to another.        |
| [skopeo-delete(1)](/docs/skopeo-delete.1.md)       | Mark the image-name for later deletion by the registry's garbage collector.                                                                |
| [skopeo-generate-sigstore-key(1)](/docs/skopeo-generate-sigstore-key.1.md)    | Generate a sigstore public/private key pair.  |
| [skopeo-inspect(1)](/docs/skopeo-inspect.1.md)     | Return  low-level  information about image-name in a registry.                                |
| [skopeo-list-tags(1)](/docs/skopeo-list-tags.1.md) | Return a list of tags for the transport-specific image repository.                               |
| [skopeo-login(1)](/docs/skopeo-login.1.md)         | Login to a container registry.                                                               |
| [skopeo-logout(1)](/docs/skopeo-logout.1.md)       | Logout of a container registry.                                                              |
| [skopeo-manifest-digest(1)](/docs/skopeo-manifest-digest.1.md)    | Compute a manifest digest for a manifest-file and write it to standard output.   |
| [skopeo-standalone-sign(1)](/docs/skopeo-standalone-sign.1.md)    | Debugging tool - Sign an image locally without uploading.                     |
| [skopeo-standalone-verify(1)](/docs/skopeo-standalone-verify.1.md)| Debugging tool - Verify an image signature from local files.                  |
| [skopeo-sync(1)](/docs/skopeo-sync.1.md)           | Synchronize images between registry repositories and local directories.                      |

License
-
skopeo is licensed under the Apache License, Version 2.0. See
[LICENSE](LICENSE) for the full license text.
