#!/bin/bash

# This script is intended to be executed by automation or humans
# under a hack/get_ci_vm.sh context.  Use under any other circumstances
# is unlikely to function.

set -e

if [[ -r "/etc/automation_environment" ]]; then
    source /etc/automation_environment
    source $AUTOMATION_LIB_PATH/common_lib.sh
else
    (
    echo "WARNING: It does not appear that containers/automation was installed."
    echo "         Functionality of most of ${BASH_SOURCE[0]} will be negatively"
    echo "         impacted."
    ) > /dev/stderr
fi

OS_RELEASE_ID="$(source /etc/os-release; echo $ID)"
# GCE image-name compatible string representation of distribution _major_ version
OS_RELEASE_VER="$(source /etc/os-release; echo $VERSION_ID | tr -d '.')"
# Combined to ease some usage
OS_REL_VER="${OS_RELEASE_ID}-${OS_RELEASE_VER}"

export "PATH=$PATH:$GOPATH/bin"

_run_setup() {
    if [[ "$OS_RELEASE_ID" == "fedora" ]]; then
        # This is required as part of the standard Fedora VM setup
        growpart /dev/sda 1
        resize2fs /dev/sda1

        # VM's come with the distro. skopeo package pre-installed
        dnf erase -y skopeo

        bash ./hack/test_env_setup.sh
    else
        die "Unknown/unsupported distro. $OS_REL_VER"
    fi
}

_run_vendor() {
    make vendor BUILDTAGS="$BUILDTAGS"
}

_run_build() {
    make bin/skopeo BUILDTAGS="$BUILDTAGS"
}

_run_cross() {
    make local-cross BUILDTAGS="$BUILDTAGS"
}

_run_validate() {
    make validate-local BUILDTAGS="$BUILDTAGS"
}

_run_unit() {
    make test-unit-local BUILDTAGS="$BUILDTAGS"
}

_run_integration() {
    make test-integration-local BUILDTAGS="$BUILDTAGS"
}

_run_system() {
    # Ensure we start with a clean-slate
    podman system reset --force
    # Executes with containers required for testing.
    showrun make test-system-local BUILDTAGS="$BUILDTAGS"
}

req_env_vars SKOPEO_PATH BUILDTAGS

handler="_run_${1}"
if [ "$(type -t $handler)" != "function" ]; then
    die "Unknown/Unsupported command-line argument '$1'"
fi

msg "************************************************************"
msg "Runner executing $1 on $OS_REL_VER"
msg "************************************************************"

cd "$SKOPEO_PATH"
$handler
