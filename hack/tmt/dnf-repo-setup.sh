#!/usr/bin/env bash

set -exo pipefail

COPR_REPO_FILE=$(compgen -G "/etc/yum.repos.d/*podman-next*.repo")
if [[ -n "$COPR_REPO_FILE" ]]; then
    # shellcheck disable=SC2016
    sed -i -n '/^priority=/!p;$apriority=1' "${COPR_REPO_FILE}"
fi
dnf -y upgrade --allowerasing
