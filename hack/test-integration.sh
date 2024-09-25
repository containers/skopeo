#!/bin/bash
set -exo pipefail

if [[ ! -f /usr/bin/skopeo ]]; then
    make PREFIX=/usr install
fi

echo "cd ./integration;" go test $TESTFLAGS ${BUILDTAGS:+-tags "$BUILDTAGS"}
cd ./integration
go test $TESTFLAGS ${BUILDTAGS:+-tags "$BUILDTAGS"}
