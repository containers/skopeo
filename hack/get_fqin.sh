#!/usr/bin/env bash

# This script is intended to be called from the Makefile.  It's purpose
# is to automation correspondence between the environment used for local
# development and CI.

set -e

SCRIPT_FILEPATH=$(realpath "${BASH_SOURCE[0]}")
SCRIPT_DIRPATH=$(dirname "$SCRIPT_FILEPATH")
REPO_DIRPATH=$(realpath "$SCRIPT_DIRPATH/../")

# When running under CI, we already have the necessary information,
# simply provide it to the Makefile.
if [[ -n "$SKOPEO_CIDEV_CONTAINER_FQIN" ]]; then
    echo "$SKOPEO_CIDEV_CONTAINER_FQIN"
    exit 0
fi

if [[ -n $(command -v podman) ]]; then CONTAINER_RUNTIME=podman; fi
CONTAINER_RUNTIME=${CONTAINER_RUNTIME:-docker}

# Borrow the get_ci_vm container image since it's small, and
# by necessity contains a script that can accurately interpret
# env. var. values from any .cirrus.yml runtime context.
$CONTAINER_RUNTIME run --rm \
    --security-opt label=disable \
    -v $REPO_DIRPATH:/src:ro \
    --entrypoint=/usr/share/automation/bin/cirrus-ci_env.py \
    quay.io/libpod/get_ci_vm:latest \
    --envs="Skopeo Test" /src/.cirrus.yml | \
    egrep -m1 '^SKOPEO_CIDEV_CONTAINER_FQIN' | \
    awk -F "=" -e '{print $2}' | \
    tr -d \'\"
