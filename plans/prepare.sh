#!/usr/bin/env bash

set -eox pipefail

RHEL_RELEASE=$(rpm --eval %{?rhel})
ARCH=$(uname -m)

# disable container-tools module on el8
if [ $RHEL_RELEASE -eq 8 ]; then
    dnf -y module disable container-tools
fi

# install epel-release on centos stream and rhel
if [ -f /etc/centos-release ]; then
    dnf -y install epel-release
elif [ $RHEL_RELEASE -ge 8 ]; then
    dnf -y install https://dl.fedoraproject.org/pub/epel/epel-release-latest-$RHEL_RELEASE.noarch.rpm
    dnf config-manager --set-enabled epel
fi

# Some envs like containers don't have the copr plugin installed
dnf -y install 'dnf-command(copr)'

# Enable podman-next copr
dnf -y copr enable rhcontainerbot/podman-next

# Set podman-next to higher priority than default
dnf config-manager --save --setopt="*:rhcontainerbot:podman-next.priority=5"
