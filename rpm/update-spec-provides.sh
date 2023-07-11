#!/usr/bin/env bash

# This script will update the goimports in the rpm spec for downstream fedora
# packaging, via the `propose-downstream` packit action.
# The goimports don't need to be present upstream.

set -eo pipefail
set -x

PACKAGE=skopeo
# script is run from git root directory
SPEC_FILE=rpm/$PACKAGE.spec

# Needed for golist to work
# This is run in the packit sandbox environment for downstream tasks and
# doesn't affect upstream repo and CI.
export GOPATH=~/go
GOPATHDIR=$GOPATH/src/github.com/containers/
mkdir -p $GOPATHDIR
ln -sf $(pwd) $GOPATHDIR/.

# Packit sandbox doesn't allow root
# Install golist by downloading and extracting rpm
# We could handle this in packit `sandcastle` upstream itself
# but that depends on golist existing in epel
# https://github.com/packit/sandcastle/pull/186
dnf download golist
rpm2cpio golist-*.rpm | cpio -idmv

sed -i '/Provides: bundled(golang.*/d' $SPEC_FILE

GO_IMPORTS=$(./usr/bin/golist --imported --package-path github.com/containers/$PACKAGE --skip-self | sort -u | xargs "-I{}" echo "Provides: bundled(golang({}))")

awk -v r="$GO_IMPORTS" '/^# vendored libraries/ {print; print r; next} 1' $SPEC_FILE > temp && mv temp $SPEC_FILE
