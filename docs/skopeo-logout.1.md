% skopeo-logout(1)

## NAME
skopeo\-logout - Logout of a container registry.

## SYNOPSIS
**skopeo logout** [*options*] _registry_

## DESCRIPTION
**skopeo logout** logs out of a specified registry server by deleting the cached credentials
stored in the **auth.json** file. The path of the credentials file can be overridden by the user by setting the **authfile** flag.
All the cached credentials can be removed by setting the **all** flag.

## OPTIONS

See also [skopeo(1)](skopeo.1.md) for options placed before the subcommand name.

**--authfile**=*path*

Path of the managed registry credentials file. On Linux, the default is ${XDG\_RUNTIME\_DIR}/containers/auth.json.
See **containers-auth.json**(5) for more details about the default on other platforms.

The default value of this option is read from the `REGISTRY\_AUTH\_FILE` environment variable.

**--compat-auth-file**=*path*

Instead of updating the default credentials file, update the one at *path*, and use a Docker-compatible format.

**--all**, **-a**

Remove the cached credentials for all registries in the auth file

**--help**, **-h**

Print usage statement

**--tls-verify**=_bool_

Require HTTPS and verify certificates when talking to the container registry or daemon. Default to registry.conf setting.

## EXAMPLES

```console
$ skopeo logout docker.io
Remove login credentials for docker.io
```

```console
$ skopeo logout --authfile authdir/myauths.json docker.io
Remove login credentials for docker.io
```

```console
$ skopeo logout --all
Remove login credentials for all registries
```

## SEE ALSO
skopeo(1), skopeo-login(1), containers-auth.json(5)

## HISTORY
May 2020, Originally compiled by Qi Wang <qiwan@redhat.com>
