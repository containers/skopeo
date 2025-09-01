#!/usr/bin/env bash

set -exo pipefail

# Install bats
# https://bats-core.readthedocs.io/en/stable/installation.html

BATS_TMPDIR=$(mktemp -d)
pushd "$BATS_TMPDIR"

BATS_VERSION=1.12.0
curl -ssfL https://github.com/bats-core/bats-core/archive/refs/tags/v"$BATS_VERSION".tar.gz | tar -xz
pushd bats-core-"$BATS_VERSION"
./install.sh /usr
popd
popd
