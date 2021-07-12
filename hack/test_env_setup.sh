

# This script is used by both Dockerfile and CI, in order to configure
# Fedora environment to execute the unit and integration tests.
# It should NEVER ever (EVER!) be used under any other circumstances
# (nor set as executable).

set -e

# Removing the source significantly reduces environment size
# when this script is used to build a container image.
# However, an existing $GOPATH may contain nuggets we can avoid
# re-downloading when running in a VM.
if [[ -z "$GOPATH" ]]; then
    echo "Error: \$GOPATH must be non-empty"
    exit 1
fi
TMPGOPATH=$(mktemp -d -p '' "$(basename ${BASH_SOURCE[0]})_XXXXXXXX")
cp --no-dereference --recursive $GOPATH --target-directory $TMPGOPATH
export GOPATH="$TMPGOPATH"

# Install three registry server versions. The first is an older version that
# only supports schema1 manifests. The second is a newer version that supports
# both.  The third is an ancient version from OpenShift Origin.
REG_REPO="https://github.com/docker/distribution.git"
REG_COMMIT="47a064d4195a9b56133891bbb13620c3ac83a827"
REG_COMMIT_SCHEMA1="ec87e9b6971d831f0eff752ddb54fb64693e51cd"
REG_GOSRC="$GOPATH/src/github.com/docker/distribution"
OSO_REPO="https://github.com/openshift/origin.git"
OSO_TAG="v1.5.0-alpha.3"
OSO_GOSRC="$GOPATH/src/github.com/openshift/origin"

# This golang code pre-dates support of go modules
export GO111MODULE=off

# Workaround unnecessary swap-enabling shenanagains in openshift-origin build
export OS_BUILD_SWAP_DISABLE=1

# Make debugging easier
set -x

git clone "$REG_REPO" "$REG_GOSRC"
cd "$REG_GOSRC"

# Don't pollute the environment
(
    # This is required to be set like this by the build system
    GOPATH="$PWD/Godeps/_workspace:$GOPATH"
    git checkout -q "$REG_COMMIT"
    go build -o /usr/local/bin/registry-v2 \
        github.com/docker/distribution/cmd/registry

    git checkout -q "$REG_COMMIT_SCHEMA1"
    go build -o /usr/local/bin/registry-v2-schema1 \
        github.com/docker/distribution/cmd/registry
)

git clone --depth 1 -b "$OSO_TAG" "$OSO_REPO" "$OSO_GOSRC"
cd "$OSO_GOSRC"

# Edit out a "go < 1.5" check which works incorrectly with go ≥ 1.10.
sed -i -e 's/\[\[ "\${go_version\[2]}" < "go1.5" ]]/false/' ./hack/common.sh

make build
make all WHAT=cmd/dockerregistry
cp -a ./_output/local/bin/linux/*/* /usr/local/bin/
cp ./images/dockerregistry/config.yml /atomic-registry-config.yml
mkdir /registry

# When script unsuccessful, leave this behind for debugging
rm -rf $GOPATH
