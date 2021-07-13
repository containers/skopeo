% skopeo-standalone-sign(1)

## NAME
skopeo\-standalone-sign - Debugging tool - Publish and sign an image in one step.

## SYNOPSIS
**skopeo standalone-sign** [*options*] _manifest_ _docker-reference_ _key-fingerprint_ **--output**|**-o** _signature_

## DESCRIPTION
This is primarily a debugging tool, or useful for special cases,
and usually should not be a part of your normal operational workflow; use `skopeo copy --sign-by` instead to publish and sign an image in one step.

  _manifest_ Path to a file containing the image manifest

  _docker-reference_ A docker reference to identify the image with

  _key-fingerprint_ Key identity to use for signing

## OPTIONS

**--help**, **-h**

Print usage statement

**--output**, **-o** _output file_

Write signature to _output file_.

## EXAMPLES

```sh
$ skopeo standalone-sign busybox-manifest.json registry.example.com/example/busybox 1D8230F6CDB6A06716E414C1DB72F2188BB46CC8 --output busybox.signature
$
```

## SEE ALSO
skopeo(1), skopeo-copy(1), containers-signature(5)

## AUTHORS

Antonio Murdaca <runcom@redhat.com>, Miloslav Trmac <mitr@redhat.com>, Jhon Honce <jhonce@redhat.com>
