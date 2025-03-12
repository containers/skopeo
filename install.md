# Installing Skopeo

## Distribution Packages
`skopeo` may already be packaged in your distribution. This document lists the
installation steps for many distros, along with their information and support links.

### Fedora

```sh
sudo dnf -y install skopeo
```

[Package Info](https://src.fedoraproject.org/rpms/skopeo) and
[Bugzilla](https://bugzilla.redhat.com/buglist.cgi?bug_status=__open__&classification=Fedora&component=skopeo&product=Fedora)

Fedora bugs can be reported on the Skopeo GitHub [Issues](https://github.com/containers/skopeo/issues) page.

### RHEL / CentOS Stream ≥ 8

```sh
sudo dnf -y install skopeo
```

If you are a RHEL customer, please reach out through the official RHEL support
channels for any issues.

CentOS Stream 9: [Package Info](https://gitlab.com/redhat/centos-stream/rpms/skopeo/-/tree/c9s) and
[Bugzilla](https://bugzilla.redhat.com/buglist.cgi?bug_status=__open__&classification=Red%20Hat&component=skopeo&product=Red%20Hat%20Enterprise%20Linux%209&version=CentOS%20Stream)

CentOS Stream 8: [Package Info](https://git.centos.org/rpms/skopeo/tree/c8s-stream-rhel8) and
[Bugzilla](https://bugzilla.redhat.com/buglist.cgi?bug_status=__open__&classification=Red%20Hat&component=skopeo&product=Red%20Hat%20Enterprise%20Linux%208&version=CentOS%20Stream)


### RHEL/CentOS ≤ 7.x

```sh
sudo yum -y install skopeo
```

CentOS 7: [Package Repo](https://git.centos.org/rpms/skopeo/tree/c7-extras)

### openSUSE

```sh
sudo zypper install skopeo
```

[Package Info](https://software.opensuse.org/package/skopeo)

### Alpine

```sh
sudo apk add skopeo
```

[Package Info](https://pkgs.alpinelinux.org/packages?name=skopeo)

### Gentoo

```sh
sudo emerge app-containers/skopeo
```

[Package Info](https://packages.gentoo.org/packages/app-containers/skopeo)

### Arch Linux

```sh
sudo pacman -S skopeo
```

[Package Info](https://archlinux.org/packages/extra/x86_64/skopeo/)

### macOS

```sh
brew install skopeo
```

### Nix / NixOS
```sh
$ nix-env -i skopeo
```

[Package Info](https://search.nixos.org/packages?&show=skopeo&query=skopeo)

### Debian

The skopeo package is available on [Bullseye](https://packages.debian.org/bullseye/skopeo),
and Debian Testing and Unstable.

```bash
# Debian Bullseye, Testing or Unstable/Sid
sudo apt-get update
sudo apt-get -y install skopeo
```

[Package Info](https://packages.debian.org/stable/skopeo)

### Raspberry Pi OS arm64 (beta)

Raspberry Pi OS uses the standard Debian's repositories,
so it is fully compatible with Debian's arm64 repository.
You can simply follow the [steps for Debian](#debian) to install Skopeo.


### Ubuntu

The skopeo package is available in the official repositories for Ubuntu 20.10
and newer.

```bash
# Ubuntu 20.10 and newer
sudo apt-get -y update
sudo apt-get -y install skopeo
```

[Package Info](https://packages.ubuntu.com/jammy/skopeo)

### Windows
Skopeo has not yet been packaged for Windows. There is an [open feature
request](https://github.com/containers/skopeo/issues/715) and contributions are
always welcome.

## Container Images

Skopeo container images are available at `quay.io/skopeo/stable:latest`.
For example,

```bash
podman run docker://quay.io/skopeo/stable:latest copy --help
```

The skopeo container image build context and automation are
located at [https://github.com/containers/image_build/tree/main/skopeo](https://github.com/containers/image_build/tree/main/skopeo)


## Building from Source

Otherwise, read on for building and installing it from source:

To build the `skopeo` binary you need at least Go 1.23.

There are two ways to build skopeo: in a container, or locally without a
container. Choose the one which better matches your needs and environment.

### Building without a container

Building without a container requires a bit more manual work and setup in your
environment, but it is more flexible:

- It should work in more environments (e.g. for native macOS builds)
- It does not require root privileges (after dependencies are installed)
- It is faster, therefore more convenient for developing `skopeo`.

Install the necessary dependencies:

```bash
# Fedora:
sudo dnf install gpgme-devel libassuan-devel btrfs-progs-devel
```

```bash
# Ubuntu (`libbtrfs-dev` requires Ubuntu 18.10 and above):
sudo apt install libgpgme-dev libassuan-dev libbtrfs-dev pkg-config
```

```bash
# macOS:
brew install gpgme
```

```bash
# openSUSE:
sudo zypper install libgpgme-devel libbtrfs-devel glib2-devel
```

```bash
# Arch Linux:
sudo pacman -S base-devel gpgme btrfs-progs
```

Make sure to clone this repository in your `GOPATH` - otherwise compilation fails.

```bash
git clone https://github.com/containers/skopeo $GOPATH/src/github.com/containers/skopeo
cd $GOPATH/src/github.com/containers/skopeo && make bin/skopeo
```

By default the `make` command (make all) will build bin/skopeo and the documentation locally.

Building of documentation requires `go-md2man`. On systems that do not have this tool, the
document generation can be skipped by passing `DISABLE_DOCS=1`:
```
DISABLE_DOCS=1 make
```

#### Additional prerequisites

In order to dynamically link against system libraries and avoid compilation errors the ```CGO_ENABLED='1'``` flag must be enabled. You can easily check by ```go env | grep CGO_ENABLED```.

An alternative would be to set the `BUILDTAGS=containers_image_openpgp` (this removes the dependency on `libgpgme` and its companion libraries).

### Cross-compilation

For cross-building skopeo, use the command `make bin/skopeo.OS.ARCH`, where OS represents
the target operating system and ARCH stands for the desired architecture. For instance,
to build skopeo for RISC-V 64-bit Linux, execute:

```bash
make bin/skopeo.linux.riscv64
```

### Building documentation

To build the manual you will need go-md2man.

```bash
# Debian:
sudo apt-get install go-md2man
```

```
# Fedora:
sudo dnf install go-md2man
```

```
# MacOS:
brew install go-md2man
```

Then

```bash
make docs
```

### Building in a container

Building in a container is simpler, but more restrictive:

- It requires the `podman` command and the ability to run Linux containers.
- The created executable is a Linux executable, and depends on dynamic libraries
  which may only be available only in a container of a similar Linux
  distribution.

```bash
$ make binary
```

### Shell completion scripts

Skopeo has shell completion scripts for bash, zsh, fish and powershell. They are installed as part of `make install`.
You may have to restart your shell in order for them to take effect.

For instructions to manually generate and load the scripts please see `skopeo completion --help`.

### Installation

Finally, after the binary and documentation is built:

```bash
sudo make install
```

### Building a static binary

There have been efforts in the past to produce and maintain static builds, but the maintainers prefer to run Skopeo using distro packages or within containers. This is because static builds of Skopeo tend to be unreliable and functionally restricted. Specifically:
- Some features of Skopeo depend on non-Go libraries like `libgpgme`.
- Generating static Go binaries uses native Go libraries, which don't support e.g. `.local` or LDAP-based name resolution.

That being said, if you would like to build Skopeo statically, you might be able to do it by combining all the following steps.
- Export environment variable `CGO_ENABLED=0` (disabling CGO causes Go to prefer native libraries when possible, instead of dynamically linking against system libraries).
- Set the `BUILDTAGS=containers_image_openpgp` Make variable (this removes the dependency on `libgpgme` and its companion libraries).
- Clear the `GO_DYN_FLAGS` Make variable if even a dependency on the ELF interpreter is undesirable.

Keep in mind that the resulting binary is unsupported and might crash randomly. Only use if you know what you're doing!

For more information, history, and context about static builds, check the following issues:

- [#391] - Consider distributing statically built binaries as part of release
- [#669] - Static build fails with segmentation violation
- [#670] - Fixing static binary build using container
- [#755] - Remove static and in-container targets from Makefile
- [#932] - Add nix derivation for static builds
- [#1336] - Unable to run skopeo on Fedora 30 (due to dyn lib dependency)
- [#1478] - Publish binary releases to GitHub (request+discussion)

[#391]: https://github.com/containers/skopeo/issues/391
[#669]: https://github.com/containers/skopeo/issues/669
[#670]: https://github.com/containers/skopeo/issues/670
[#755]: https://github.com/containers/skopeo/issues/755
[#932]: https://github.com/containers/skopeo/issues/932
[#1336]: https://github.com/containers/skopeo/issues/1336
[#1478]: https://github.com/containers/skopeo/issues/1478
