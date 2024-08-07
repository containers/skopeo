#!/usr/bin/env bash

set -exo pipefail

cat /etc/redhat-release

# Remove testing-farm repos if they exist as these interfere with the packages
# we want to install, especially when podman-next copr is involved
rm -f /etc/yum.repos.d/tag-repository.repo

# Install dependencies for running tests and ensure all packages are updated
# NOTE: bats will be fetched from Fedora repos on public testing-farm envs if EPEL repo is absent or disabled.
dnf -y install bats podman netavark
dnf -y update

# Print versions of distro and installed packages
rpm -q bats container-selinux golang netavark podman selinux-policy skopeo

make -C ../.. SKOPEO_BINARY=/usr/bin/skopeo test-system
