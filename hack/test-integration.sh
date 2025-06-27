#!/bin/bash
set -e

echo "cd ./integration;" go test "$TESTFLAGS" ${BUILDTAGS:+-tags "$BUILDTAGS"}
cd ./integration
go test "$TESTFLAGS" ${BUILDTAGS:+-tags "$BUILDTAGS"}
