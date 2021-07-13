% skopeo-manifest-digest(1)

## NAME
skopeo\-manifest\-digest - Compute a manifest digest for a manifest-file and write it to standard output.

## SYNOPSIS
**skopeo manifest-digest** _manifest-file_

## DESCRIPTION

Compute a manifest digest of _manifest-file_ and write it to standard output.

## OPTIONS

**--help**, **-h**

Print usage statement

## EXAMPLES

```sh
$ skopeo manifest-digest manifest.json
sha256:a59906e33509d14c036c8678d687bd4eec81ed7c4b8ce907b888c607f6a1e0e6
```

## SEE ALSO
skopeo(1)

## AUTHORS

Antonio Murdaca <runcom@redhat.com>, Miloslav Trmac <mitr@redhat.com>, Jhon Honce <jhonce@redhat.com>
