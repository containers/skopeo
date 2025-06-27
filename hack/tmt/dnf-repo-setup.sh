#!/usr/bin/env bash

set -exo pipefail

COPR_REPO_FILE="/etc/yum.repos.d/*podman-next*.repo"
if compgen -G "$COPR_REPO_FILE" > /dev/null; then
    # shellcheck disable=SC2016
    sed -i -n '/^priority=/!p;$apriority=1' "$COPR_REPO_FILE"
fi
dnf -y upgrade --allowerasing
