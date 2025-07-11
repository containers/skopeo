#!/bin/bash
set -e

make PREFIX=/usr install

echo "cd ./integration;" go test "$@" ${BUILDTAGS:+-tags "$BUILDTAGS"}
cd ./integration
go test "$@" ${BUILDTAGS:+-tags "$BUILDTAGS"}
