% skopeo-generate-sigstore-key(1)

## NAME
skopeo\-generate-sigstore-key - Generate a sigstore public/private key pair.

## SYNOPSIS
**skopeo generate-sigstore-key** [*options*] **--output-prefix** _prefix_

## DESCRIPTION

Generates a public/private key pair suitable for creating sigstore image signatures.
The private key is encrypted with a passphrase;
if one is not provided using an option, this command prompts for it interactively.

The private key is written to _prefix_**.private** .
The public key is written to _prefix_**.pub** .

## OPTIONS

See also [skopeo(1)](skopeo.1.md) for options placed before the subcommand name.

**--help**, **-h**

:   Print usage statement

**--output-prefix** _prefix_

:   Mandatory.
    Path prefix for the output keys (_prefix_**.private** and _prefix_**.pub**).

**--passphrase-file** _path_

:   The passphare to use to encrypt the private key.
    Only the first line will be read.
    A passphrase stored in a file is of questionable security if other users can read this file.
    Do not use this option if at all avoidable.

## EXAMPLES

```console
$ skopeo generate-sigstore-key --output-prefix mykey
```

# SEE ALSO
skopeo(1), skopeo-copy(1), containers-policy.json(5)

## AUTHORS

Miloslav Trmač <mitr@redhat.com>
