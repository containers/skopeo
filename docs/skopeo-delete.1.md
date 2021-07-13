% skopeo-delete(1)

## NAME
skopeo\-delete - Mark the _image-name_ for later deletion by the registry's garbage collector.

## SYNOPSIS
**skopeo delete** [*options*] _image-name_

Mark _image-name_ for deletion.  To release the allocated disk space, you must login to the container registry server and execute the container registry garbage collector. E.g.,

```
/usr/bin/registry garbage-collect /etc/docker-distribution/registry/config.yml

Note: sometimes the config.yml is stored in /etc/docker/registry/config.yml

If you are running the container registry inside of a container you would execute something like:

$ docker exec -it registry /usr/bin/registry garbage-collect /etc/docker-distribution/registry/config.yml

```

## OPTIONS

**--authfile** _path_

Path of the authentication file. Default is ${XDG_RUNTIME\_DIR}/containers/auth.json, which is set using `skopeo login`.
If the authorization state is not found there, $HOME/.docker/config.json is checked, which is set using `docker login`.

**--creds** _username[:password]_

Credentials for accessing the registry.

**--cert-dir** _path_

Use certificates at _path_ (*.crt, *.cert, *.key) to connect to the registry.

**--daemon-host** _host_

Use docker daemon host at _host_ (`docker-daemon:` transport only)

**--help**, **-h**

Print usage statement

**--no-creds** _bool-value_

Access the registry anonymously.

Additionally, the registry must allow deletions by setting `REGISTRY_STORAGE_DELETE_ENABLED=true` for the registry daemon.

**--registry-token** _token_

Bearer token for accessing the registry.

**--retry-times**

The number of times to retry. Retry wait time will be exponentially increased based on the number of failed attempts.

**--shared-blob-dir** _directory_

Directory to use to share blobs across OCI repositories.

## EXAMPLES

Mark image example/pause for deletion from the registry.example.com registry:
```sh
$ skopeo delete --force docker://registry.example.com/example/pause:latest
```
See above for additional details on using the command **delete**.


## SEE ALSO
skopeo(1), skopeo-login(1), docker-login(1), containers-auth.json(5)

## AUTHORS

Antonio Murdaca <runcom@redhat.com>, Miloslav Trmac <mitr@redhat.com>, Jhon Honce <jhonce@redhat.com>
