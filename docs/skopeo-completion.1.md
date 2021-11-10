% skopeo-completion(1)

## NAME
skopeo\-completion - Generate shell completions.

## SYNOPSIS
**skopeo completion** [command]

## DESCRIPTION
Generate shell completions for skopeo. Takes the name of the shell as an
argument, and prints the completion file to stdout.

## OPTIONS

**bash**

Generate bash completions.

**fish**

Generate fish completions.

**zsh**

Generate zsh completions.

**powershell**

Generate powershell completions.

## EXAMPLES
To print bash completions for skopeo to stdout:
```sh
$ skopeo completion bash
```

To print fish completions for skopeo to stdout:
```sh
$ skopeo completion fish
```

To print zsh completions for skopeo to stdout:
```sh
$ skopeo completion zsh
```

To print powershell completions for skopeo to stdout:
```sh
$ skopeo completion powershell
```

## SEE ALSO
skopeo(1)

## AUTHORS
Antonio Murdaca <runcom@redhat.com>, Miloslav Trmac <mitr@redhat.com>, Jhon Honce <jhonce@redhat.com>, Lokesh Mandvekar <lsm5@fedoraproject.org>
