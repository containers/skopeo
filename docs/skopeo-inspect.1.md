% skopeo-inspect(1)

## NAME
skopeo\-inspect - Return low-level information about _image-name_ in a registry.

## SYNOPSIS
**skopeo inspect** [*options*] _image-name_

## DESCRIPTION

Return low-level information about _image-name_ in a registry

_image-name_ name of image to retrieve information about

## OPTIONS

**--authfile** _path_

Path of the authentication file. Default is ${XDG\_RUNTIME\_DIR}/containers/auth.json, which is set using `skopeo login`.
If the authorization state is not found there, $HOME/.docker/config.json is checked, which is set using `docker login`.

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

The number of times to retry; retry wait time will be exponentially increased based on the number of failed attempts.

**--shared-blob-dir** _directory_

Directory to use to share blobs across OCI repositories.

## EXAMPLES

To review information for the image fedora from the docker.io registry:
```sh
$ skopeo inspect docker://docker.io/fedora
{
    "Name": "docker.io/library/fedora",
    "Digest": "sha256:a97914edb6ba15deb5c5acf87bd6bd5b6b0408c96f48a5cbd450b5b04509bb7d",
    "RepoTags": [
	"20",
	"21",
	"22",
	"23",
	"24",
	"heisenbug",
	"latest",
	"rawhide"
    ],
    "Created": "2016-06-20T19:33:43.220526898Z",
    "DockerVersion": "1.10.3",
    "Labels": {},
    "Architecture": "amd64",
    "Os": "linux",
    "Layers": [
	"sha256:7c91a140e7a1025c3bc3aace4c80c0d9933ac4ee24b8630a6b0b5d8b9ce6b9d4"
    ]
}
```

```
$ /bin/skopeo inspect --config docker://registry.fedoraproject.org/fedora --format "{{ .Architecture }}"
amd64
```

```
$ /bin/skopeo inspect --format '{{ .Env }}' docker://registry.access.redhat.com/ubi8
[PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin container=oci]
```

# SEE ALSO
skopeo(1), skopeo-login(1), docker-login(1), containers-auth.json(5)

## AUTHORS

Antonio Murdaca <runcom@redhat.com>, Miloslav Trmac <mitr@redhat.com>, Jhon Honce <jhonce@redhat.com>
