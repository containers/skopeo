# Installing from packages

## Distribution Packages
`skopeo` may already be packaged in your distribution.

### Fedora

```sh
sudo dnf -y install skopeo
```

### RHEL/CentOS ≥ 8 and CentOS Stream

```sh
sudo dnf -y install skopeo
```

Newer Skopeo releases may be available on the repositories provided by the
Kubic project. Beware, these may not be suitable for production environments.

on CentOS 8:

```sh
sudo dnf -y module disable container-tools
sudo dnf -y install 'dnf-command(copr)'
sudo dnf -y copr enable rhcontainerbot/container-selinux
sudo curl -L -o /etc/yum.repos.d/devel:kubic:libcontainers:stable.repo https://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/stable/CentOS_8/devel:kubic:libcontainers:stable.repo
sudo dnf -y install skopeo
```

on CentOS 8 Stream:

```sh
sudo dnf -y module disable container-tools
sudo dnf -y install 'dnf-command(copr)'
sudo dnf -y copr enable rhcontainerbot/container-selinux
sudo curl -L -o /etc/yum.repos.d/devel:kubic:libcontainers:stable.repo https://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/stable/CentOS_8_Stream/devel:kubic:libcontainers:stable.repo
sudo dnf -y install skopeo
```

### RHEL/CentOS ≤ 7.x

```sh
sudo yum -y install skopeo
```

### openSUSE

```sh
sudo zypper install skopeo
```

### Alpine

```sh
sudo apk add skopeo
```

### macOS

```sh
brew install skopeo
```

### Nix / NixOS
```sh
$ nix-env -i skopeo
```

### Debian

The skopeo package is available in
the [Bullseye (testing) branch](https://packages.debian.org/bullseye/skopeo), which
will be the next stable release (Debian 11) as well as Debian Unstable/Sid.

```bash
# Debian Testing/Bullseye or Unstable/Sid
sudo apt-get update
sudo apt-get -y install skopeo
```

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

If you would prefer newer (though not as well-tested) packages,
the [Kubic project](https://build.opensuse.org/package/show/devel:kubic:libcontainers:stable/skopeo)
provides packages for active Ubuntu releases 20.04 and newer (it should also work with direct derivatives like Pop!\_OS).
Checkout the [Kubic project page](https://build.opensuse.org/package/show/devel:kubic:libcontainers:stable/skopeo)
for a list of supported Ubuntu version and
architecture combinations. **NOTE:** The command `sudo apt-get -y upgrade`
maybe required in some cases if Skopeo cannot be installed without it.
The build sources for the Kubic packages can be found [here](https://gitlab.com/rhcontainerbot/skopeo/-/tree/debian/debian).

CAUTION: On Ubuntu 20.10 and newer, we highly recommend you use Buildah, Podman and Skopeo ONLY from EITHER the Kubic repo
OR the official Ubuntu repos. Mixing and matching may lead to unpredictable situations including installation conflicts.

```bash
. /etc/os-release
echo "deb https://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/stable/xUbuntu_${VERSION_ID}/ /" | sudo tee /etc/apt/sources.list.d/devel:kubic:libcontainers:stable.list
curl -L https://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/stable/xUbuntu_${VERSION_ID}/Release.key | sudo apt-key add -
sudo apt-get update
sudo apt-get -y upgrade
sudo apt-get -y install skopeo
```


Otherwise, read on for building and installing it from source:

To build the `skopeo` binary you need at least Go 1.12.

There are two ways to build skopeo: in a container, or locally without a
container. Choose the one which better matches your needs and environment.

## Building from Source

### Building without a container

Building without a container requires a bit more manual work and setup in your
environment, but it is more flexible:

- It should work in more environments (e.g. for native macOS builds)
- It does not require root privileges (after dependencies are installed)
- It is faster, therefore more convenient for developing `skopeo`.

Install the necessary dependencies:

```bash
# Fedora:
sudo dnf install gpgme-devel libassuan-devel btrfs-progs-devel device-mapper-devel
```

```bash
# Ubuntu (`libbtrfs-dev` requires Ubuntu 18.10 and above):
sudo apt install libgpgme-dev libassuan-dev libbtrfs-dev libdevmapper-dev
```

```bash
# macOS:
brew install gpgme
```

```bash
# openSUSE:
sudo zypper install libgpgme-devel device-mapper-devel libbtrfs-devel glib2-devel
```

For newer versions of Go configured with modules support, after installing the
above prereqs, you can build and install Skopeo using a simple `go
get`.  (For more information about module support and related environment
configuration in Go 1.16, see [New module changes in Go 1.16](https://blog.golang.org/go116-module-changes).  
For earlier Go versions, see [Go Modules in 2019](https://blog.golang.org/modules2019) 
and [Using Go modules](https://blog.golang.org/using-go-modules).)

```
go get -v github.com/containers/skopeo/cmd/skopeo
```

For older versions of Go, you will need to clone this repository in your `GOPATH` - otherwise compilation fails.

```bash
git clone https://github.com/containers/skopeo $GOPATH/src/github.com/containers/skopeo
cd $GOPATH/src/github.com/containers/skopeo && make bin/skopeo
```

By default the `make` command (make all) will build bin/skopeo and the documentation locally.

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

### Installation

Finally, after the binary and documentation is built:

```bash
sudo make install
```
