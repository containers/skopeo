% skopeo-login(1)

## NAME
skopeo\-login - Login to a container registry.

## SYNOPSIS
**skopeo login** [*options*] _registry_

## DESCRIPTION
**skopeo login** logs into a specified registry server with the correct username
and password. **skopeo login** reads in the username and password from STDIN.
The username and password can also be set using the **username** and **password** flags.
The path of the authentication file can be specified by the user by setting the **authfile**
flag. The default path used is **${XDG\_RUNTIME\_DIR}/containers/auth.json**.

## OPTIONS

**--password**, **-p**=*password*

Password for registry

**--password-stdin**

Take the password from stdin

**--username**, **-u**=*username*

Username for registry

**--authfile**=*path*

Path of the authentication file. Default is ${XDG\_RUNTIME\_DIR}/containers/auth.json

Note: You can also override the default path of the authentication file by setting the REGISTRY\_AUTH\_FILE
environment variable. `export REGISTRY_AUTH_FILE=path`

**--get-login**

Return the logged-in user for the registry. Return error if no login is found.

**--cert-dir**=*path*

Use certificates at *path* (\*.crt, \*.cert, \*.key) to connect to the registry.
Default certificates directory is _/etc/containers/certs.d_.

**--help**, **-h**

Print usage statement

**--verbose**, **-v**

Write more detailed information to stdout

## EXAMPLES

```
$ skopeo login docker.io
Username: testuser
Password:
Login Succeeded!
```

```
$ skopeo login -u testuser -p testpassword localhost:5000
Login Succeeded!
```

```
$ skopeo login --authfile authdir/myauths.json docker.io
Username: testuser
Password:
Login Succeeded!
```

```
$ skopeo login --tls-verify=false -u test -p test localhost:5000
Login Succeeded!
```

```
$ skopeo login --cert-dir /etc/containers/certs.d/ -u foo -p bar localhost:5000
Login Succeeded!
```

```
$ skopeo login -u testuser  --password-stdin < testpassword.txt docker.io
Login Succeeded!
```

```
$ echo $testpassword | skopeo login -u testuser --password-stdin docker.io
Login Succeeded!
```

## SEE ALSO
skopeo(1), skopeo-logout(1), containers-auth.json(5), containers-registries.conf(5), containers-certs.d.5.md

## HISTORY
May 2020, Originally compiled by Qi Wang <qiwan@redhat.com>
