% skopeo-list-tags(1)

## NAME
skopeo\-list\-tags - List image names in a transport-specific collection of images.

## SYNOPSIS
**skopeo list-tags** [*options*] _source-image_

Return a list of tags from _source-image_ in a registry or a local docker-archive file.

  _source-image_ name of the repository to retrieve a tag listing from or a local docker-archive file.

## OPTIONS

See also [skopeo(1)](skopeo.1.md) for options placed before the subcommand name.

**--authfile** _path_

Path of the updated registry credentials file. On Linux, the default is ${XDG\_RUNTIME\_DIR}/containers/auth.json.
See **containers-auth.json**(5) for more details about the credential search mechanism and defaults on other platforms.

Use `skopeo login` to manage the credentials.

The default value of this option is read from the `REGISTRY\_AUTH\_FILE` environment variable.

**--creds** _username[:password]_ for accessing the registry.

**--cert-dir** _path_

Use certificates at _path_ (\*.crt, \*.cert, \*.key) to connect to the registry.

**--help**, **-h**

Print usage statement

**--no-creds**

Access the registry anonymously.

**--registry-token** _Bearer token_

Bearer token for accessing the registry.

**--retry-times**

The number of times to retry.

**--retry-delay**

Fixed delay between retries. If not set (or set to 0s), retry wait time will be exponentially increased based on the number of failed attempts.

**--tls-verify**=_bool_

Require HTTPS and verify certificates when talking to the container registry or daemon. Default to registry.conf setting.

**--username**

The username to access the registry.

**--password**

The password to access the registry.

## REPOSITORY NAMES

Repository names are transport-specific references as each transport may have its own concept of a "repository" and "tags".

This commands refers to repositories using a _transport_`:`_details_ format. The following formats are supported:

  **docker://**_docker-repository-reference_
  A repository in a registry implementing the "Docker Registry HTTP API V2".
  A _docker-repository-reference_ is of the form: **registryhost:port/repositoryname** which is similar to an _image-reference_ but with no tag or digest allowed as the last component (e.g no `:latest` or `@sha256:xyz`)

      Examples of valid docker-repository-references:
        "docker.io/myuser/myrepo"
        "docker.io/nginx"
        "docker.io/library/fedora"
        "localhost:5000/myrepository"

      Examples of invalid references:
        "docker.io/nginx:latest"
        "docker.io/myuser/myimage:v1.0"
        "docker.io/myuser/myimage@sha256:f48c4cc192f4c3c6a069cb5cca6d0a9e34d6076ba7c214fd0cc3ca60e0af76bb"

  **docker-archive:path[:docker-reference]
  more than one images were stored in a docker save-formatted file. 

## EXAMPLES

### Docker Transport
To get the list of tags in the "fedora" repository from the docker.io registry (the repository name expands to "library/fedora" per docker transport canonical form):
```console
$ skopeo list-tags docker://docker.io/fedora
{
    "Repository": "docker.io/library/fedora",
    "Tags": [
        "20",
        "21",
        "22",
        "23",
        "24",
        "25",
        "26-modular",
        "26",
        "27",
        "28",
        "29",
        "30",
        "31",
        "32",
        "branched",
        "heisenbug",
        "latest",
        "modular",
        "rawhide"
    ]
}

```

To list the tags in a local host docker/distribution registry on port 5000, in this case for the "fedora" repository:

```console
$ skopeo list-tags docker://localhost:5000/fedora
{
    "Repository": "localhost:5000/fedora",
    "Tags": [
        "latest",
        "30",
        "31"
    ]
}

```

### Docker-archive Transport

To list the tags in a local docker-archive file:

```console
$ skopeo list-tags docker-archive:/tmp/busybox.tar.gz
{
    "Tags": [
        "busybox:1.28.3"
    ]
}
```

Also supports more than one tags in an archive:

```console
$ skopeo list-tags docker-archive:/tmp/docker-two-images.tar.gz
{
    "Tags": [
        "example.com/empty:latest",
        "example.com/empty/but:different"
    ]
}
```

Will include a source-index entry for each untagged image:

```console
$ skopeo list-tags docker-archive:/tmp/four-tags-with-an-untag.tar
{
    "Tags": [
        "image1:tag1",
        "image2:tag2",
        "@2",
        "image4:tag4"
    ]
}
```


# SEE ALSO
skopeo(1), skopeo-login(1), docker-login(1), containers-auth.json(5), containers-transports(1)

## AUTHORS

Zach Hill <zach@anchore.com>
