% skopeo-standalone-verify(1)

## NAME
skopeo\-standalone\-verify - Verify an image signature.

## SYNOPSIS
**skopeo standalone-verify** _manifest_ _docker-reference_ _key-fingerprint_ _signature_

## DESCRIPTION

Verify a signature using local files, digest will be printed on success.

  _manifest_ Path to a file containing the image manifest

  _docker-reference_ A docker reference expected to identify the image in the signature

  _key-fingerprint_ Expected identity of the signing key

  _signature_ Path to signature file

**Note:** If you do use this, make sure that the image can not be changed at the source location between the times of its verification and use.

## OPTIONS

**--help**, **-h**

Print usage statement

## EXAMPLES

```sh
$ skopeo standalone-verify busybox-manifest.json registry.example.com/example/busybox 1D8230F6CDB6A06716E414C1DB72F2188BB46CC8  busybox.signature
Signature verified, digest sha256:20bf21ed457b390829cdbeec8795a7bea1626991fda603e0d01b4e7f60427e55
```

## SEE ALSO
skopeo(1), containers-signature(5)

## AUTHORS

Antonio Murdaca <runcom@redhat.com>, Miloslav Trmac <mitr@redhat.com>, Jhon Honce <jhonce@redhat.com>
