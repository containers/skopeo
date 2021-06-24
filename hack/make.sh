#!/usr/bin/env bash
set -e

# This script builds various binary from a checkout of the skopeo
# source code.
#
# Requirements:
# - The current directory should be a checkout of the skopeo source code
#   (https://github.com/containers/skopeo). Whatever version is checked out
#   will be built.
# - The script is intended to be run inside the docker container specified
#   in the Dockerfile at the root of the source. In other words:
#   DO NOT CALL THIS SCRIPT DIRECTLY.
# - The right way to call this script is to invoke "make" from
#   your checkout of the skopeo repository.
#   the Makefile will do a "docker build -t skopeo ." and then
#   "docker run hack/make.sh" in the resulting image.
#

set -o pipefail

export SKOPEO_PKG='github.com/containers/skopeo'
export SCRIPTDIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
export MAKEDIR="$SCRIPTDIR/make"

# Override this to "0" to disable tests which may fail w/o
# having applied hack/test_env_setup.sh
export SKOPEO_CONTAINER_TESTS=${SKOPEO_CONTAINER_TESTS:-1}

echo

# List of bundles to create when no argument is passed
# TODO(runcom): these are the one left from Docker...for now
# test-unit
# validate-dco
# cover
DEFAULT_BUNDLES=(
	validate-gofmt
	validate-lint
	validate-vet
	validate-git-marks

	test-integration
)

TESTFLAGS+=" -test.timeout=15m"

# Go module support: set `-mod=vendor` to use the vendored sources
# See also the top-level Makefile.
mod_vendor=
if go help mod >/dev/null 2>&1; then
  export GO111MODULE=on
  mod_vendor='-mod=vendor'
fi

# If $TESTFLAGS is set in the environment, it is passed as extra arguments to 'go test'.
# You can use this to select certain tests to run, eg.
#
#     TESTFLAGS='-test.run ^TestBuild$' ./hack/make.sh test-unit
#
# For integration-cli test, we use [gocheck](https://labix.org/gocheck), if you want
# to run certain tests on your local host, you should run with command:
#
#     TESTFLAGS='-check.f DockerSuite.TestBuild*' ./hack/make.sh binary test-integration-cli
#
go_test_dir() {
	dir=$1
	(
		echo '+ go test' $mod_vendor $TESTFLAGS ${BUILDTAGS:+-tags "$BUILDTAGS"} "${SKOPEO_PKG}${dir#.}"
		cd "$dir"
		export DEST="$ABS_DEST" # we're in a subshell, so this is safe -- our integration-cli tests need DEST, and "cd" screws it up
		go test $mod_vendor $TESTFLAGS ${BUILDTAGS:+-tags "$BUILDTAGS"}
	)
}

bundle() {
	local bundle="$1"; shift
	echo "---> Making bundle: $(basename "$bundle")"
	source "$SCRIPTDIR/make/$bundle" "$@"
}

main() {
	if [ $# -lt 1 ]; then
		bundles=(${DEFAULT_BUNDLES[@]})
	else
		bundles=($@)
	fi
	for bundle in ${bundles[@]}; do
		bundle "$bundle"
		echo
	done
}

main "$@"
