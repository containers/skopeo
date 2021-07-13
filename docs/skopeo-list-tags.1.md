% skopeo-list-tags(1)

## NAME
skopeo\-list\-tags - List tags in the transport-specific image repository.

## SYNOPSIS
**skopeo list-tags** [*options*] _repository-name_

Return a list of tags from _repository-name_ in a registry.

  _repository-name_ name of repository to retrieve tag listing from

## OPTIONS

**--authfile** _path_

Path of the authentication file. Default is ${XDG\_RUNTIME\_DIR}/containers/auth.json, which is set using `skopeo login`.
  If the authorization state is not found there, $HOME/.docker/config.json is checked, which is set using `docker login`.

**--creds** _username[:password]_ for accessing the registry.

**--cert-dir** _path_

Use certificates at _path_ (\*.crt, \*.cert, \*.key) to connect to the registry.

**--help**, **-h**

Print usage statement

**--no-creds** _bool-value_

Access the registry anonymously.

**--registry-token** _Bearer token_

Bearer token for accessing the registry.

**--retry-times**

The number of times to retry. Retry wait time will be exponentially increased based on the number of failed attempts.

## REPOSITORY NAMES

Repository names are transport-specific references as each transport may have its own concept of a "repository" and "tags". Currently, only the Docker transport is supported.

This commands refers to repositories using a _transport_`:`_details_ format. The following formats are supported:

  **docker://**_docker-repository-reference_
  A repository in a registry implementing the "Docker Registry HTTP API V2". By default, uses the authorization state in either `$XDG_RUNTIME_DIR/containers/auth.json`, which is set using `(skopeo login)`. If the authorization state is not found there, `$HOME/.docker/config.json` is checked, which is set using `(docker login)`.
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


## EXAMPLES

### Docker Transport
To get the list of tags in the "fedora" repository from the docker.io registry (the repository name expands to "library/fedora" per docker transport canonical form):
```sh
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

```sh
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

# SEE ALSO
skopeo(1), skopeo-login(1), docker-login(1), containers-auth.json(5)

## AUTHORS

Zach Hill <zach@anchore.com>
