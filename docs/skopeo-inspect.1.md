% skopeo-inspect(1)

## NAME
skopeo\-inspect - Return low-level information about _image-name_ in a registry.

## SYNOPSIS
**skopeo inspect** [*options*] _image-name_

## DESCRIPTION

Return low-level information about _image-name_ in a registry.
See [skopeo(1)](skopeo.1.md) for the format of _image-name_.

The default output includes data from various sources: user input (**Name**), the remote repository, if any (**RepoTags**), the top-level manifest (**Digest**),
and a per-architecture/OS image matching the current run-time environment (most other values).
To see values for a different architecture/OS, use the **--override-os** / **--override-arch** options documented in [skopeo(1)](skopeo.1.md).

## OPTIONS

See also [skopeo(1)](skopeo.1.md) for options placed before the subcommand name.

**--authfile** _path_

Path of the primary registry credentials file. On Linux, the default is ${XDG\_RUNTIME\_DIR}/containers/auth.json.
See **containers-auth.json**(5) for more details about the credential search mechanism and defaults on other platforms.

Use `skopeo login` to manage the credentials.

The default value of this option is read from the `REGISTRY\_AUTH\_FILE` environment variable.

**--cert-dir** _path_

Use certificates at _path_ (\*.crt, \*.cert, \*.key) to connect to the registry.

**--config**

Output configuration in OCI format, default is to format in JSON format.

**--creds** _username[:password]_

Username and password for accessing the registry.

**--daemon-host** _host_

Use docker daemon host at _host_ (`docker-daemon:` transport only)

**--format**, **-f**=*format*

Format the output using the given Go template.
The keys of the returned JSON can be used as the values for the --format flag (see examples below).
Supports the Go templating functions available at https://pkg.go.dev/github.com/containers/common/pkg/report#hdr-Template_Functions

**--help**, **-h**

Print usage statement

**--no-creds**

Access the registry anonymously.

**--raw**

Output raw manifest or config data depending on --config option.
The --format option is not supported with --raw option.

**--registry-token** _Bearer token_

Registry token for accessing the registry.

**--retry-times**

The number of times to retry.

**--retry-delay**

Fixed delay between retries. If not set (or set to 0s), retry wait time will be exponentially increased based on the number of failed attempts.

**--shared-blob-dir** _directory_

Directory to use to share blobs across OCI repositories.

**--tls-verify**=_bool_

Require HTTPS and verify certificates when talking to the container registry or daemon. Default to registry.conf setting.

**--username**

The username to access the registry.

**--password**

The password to access the registry.

**--no-tags**, **-n**

Do not list the available tags from the repository in the output. When `true`, the `RepoTags` array will be empty.  Defaults to `false`, which includes all available tags.

## EXAMPLES

To review information for the image fedora from the docker.io registry:
```console
$ skopeo inspect docker://docker.io/fedora

{
    "Name": "docker.io/library/fedora",
    "Digest": "sha256:f99efcddc4dd6736d8a88cc1ab6722098ec1d77dbf7aed9a7a514fc997ca08e0",
    "RepoTags": [
        "20",
        "21",
        "..."
    ],
    "Created": "2022-11-16T07:26:42.618327645Z",
    "DockerVersion": "20.10.12",
    "Labels": {
        "maintainer": "Clement Verna \u003ccverna@fedoraproject.org\u003e"
    },
    "Architecture": "amd64",
    "Os": "linux",
    "Layers": [
        "sha256:cb8b1ed77979b894115a983f391465651aa7eb3edd036be4b508eea47271eb93"
    ],
    "LayersData": [
        {
            "MIMEType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
            "Digest": "sha256:cb8b1ed77979b894115a983f391465651aa7eb3edd036be4b508eea47271eb93",
            "Size": 65990920,
            "Annotations": null
        }
    ],
    "Env": [
        "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
        "DISTTAG=f37container",
        "FGC=f37",
        "FBR=f37"
    ]
}
```

To inspect python from the docker.io registry and not show the available tags:
```console
$ skopeo inspect --no-tags docker://docker.io/library/python
{
    "Name": "docker.io/library/python",
    "Digest": "sha256:10fc14aa6ae69f69e4c953cffd9b0964843d8c163950491d2138af891377bc1d",
    "RepoTags": [],
    "Created": "2022-11-16T06:55:28.566254104Z",
    "DockerVersion": "20.10.12",
    "Labels": null,
    "Architecture": "amd64",
    "Os": "linux",
    "Layers": [
        "sha256:a8ca11554fce00d9177da2d76307bdc06df7faeb84529755c648ac4886192ed1",
        "sha256:e4e46864aba2e62ba7c75965e4aa33ec856ee1b1074dda6b478101c577b63abd",
        "..."
    ],
    "LayersData": [
        {
            "MIMEType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
            "Digest": "sha256:a8ca11554fce00d9177da2d76307bdc06df7faeb84529755c648ac4886192ed1",
            "Size": 55038615,
            "Annotations": null
        },
        {
            "MIMEType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
            "Digest": "sha256:e4e46864aba2e62ba7c75965e4aa33ec856ee1b1074dda6b478101c577b63abd",
            "Size": 5164893,
            "Annotations": null
        },
        "..."
    ],
    "Env": [
        "PATH=/usr/local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
        "LANG=C.UTF-8",
        "...",
    ]
}
```

```console
$ /bin/skopeo inspect --config docker://registry.fedoraproject.org/fedora --format "{{ .Architecture }}"
amd64
```

```console
$ /bin/skopeo inspect --format '{{ .Env }}' docker://registry.access.redhat.com/ubi8
[PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin container=oci]
```

# SEE ALSO
skopeo(1), skopeo-login(1), docker-login(1), containers-auth.json(5)

## AUTHORS

Antonio Murdaca <runcom@redhat.com>, Miloslav Trmac <mitr@redhat.com>, Jhon Honce <jhonce@redhat.com>
