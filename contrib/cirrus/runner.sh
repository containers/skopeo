#!/bin/bash

# This script is intended to be executed by automation or humans
# under a hack/get_ci_vm.sh context.  Use under any other circumstances
# is unlikely to function.

set -e

# BEGIN Global export of all variables
set -a

# Due to differences across platforms and runtime execution environments,
# handling of the (otherwise) default shell setup is non-uniform.  Rather
# than attempt to workaround differences, simply force-load/set required
# items every time this library is utilized.
USER="$(whoami)"
HOME="$(getent passwd $USER | cut -d : -f 6)"
# Some platforms set and make this read-only
[[ -n "$UID" ]] || \
    UID=$(getent passwd $USER | cut -d : -f 3)

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

# This is the magic interpreted by the tests to allow modifying local config/services.
SKOPEO_CONTAINER_TESTS=1

PATH=$PATH:$GOPATH/bin

# END Global export of all variables
set +a


_run_setup() {
    local mnt
    local errmsg
    req_env_vars SKOPEO_CIDEV_CONTAINER_FQIN
    if [[ "$OS_RELEASE_ID" != "fedora" ]]; then
        die "Unknown/unsupported distro. $OS_REL_VER"
    fi

    if [[ -r "/.ci_setup_complete" ]]; then
        warn "Thwarted an attempt to execute setup more than once."
        return
    fi

    # VM's come with the distro. skopeo package pre-installed
    dnf remove -y skopeo

    msg "Removing systemd-resolved from nsswitch.conf"
    # /etc/resolv.conf is already set to bypass systemd-resolvd
    sed -i -r -e 's/^(hosts.+)resolve.+dns/\1dns/' /etc/nsswitch.conf

    # A slew of compiled binaries are pre-built and distributed
    # within the CI/Dev container image, but we want to run
    # things directly on the host VM.  Fortunately they're all
    # located in the container under /usr/local/bin
    msg "Accessing contents of $SKOPEO_CIDEV_CONTAINER_FQIN"
    podman pull --retry 3 --quiet $SKOPEO_CIDEV_CONTAINER_FQIN
    mnt=$(podman mount $(podman create $SKOPEO_CIDEV_CONTAINER_FQIN))

    # The container and VM images are built in tandem in the same repo.
    # automation, but the sources are in different directories.  It's
    # possible for a mismatch to happen, but should (hopefully) be unlikely.
    # Double-check to make sure.
    if ! fgrep -qx "ID=$OS_RELEASE_ID" $mnt/etc/os-release || \
       ! fgrep -qx "VERSION_ID=$OS_RELEASE_VER" $mnt/etc/os-release; then
            die "Somehow $SKOPEO_CIDEV_CONTAINER_FQIN is not based on $OS_REL_VER."
    fi
    msg "Copying test binaries from $SKOPEO_CIDEV_CONTAINER_FQIN /usr/local/bin/"
    cp -a "$mnt/usr/local/bin/"* "/usr/local/bin/"
    msg "Configuring the openshift registry"

    # TODO: Put directory & yaml into more sensible place + update integration tests
    mkdir -vp /registry
    cp -a "$mnt/atomic-registry-config.yml" /

    msg "Cleaning up"
    podman umount --latest
    podman rm --latest

    # Ensure setup can only run once
    touch "/.ci_setup_complete"
}

_run_vendor() {
    make vendor BUILDTAGS="$BUILDTAGS"
}

_run_build() {
    make bin/skopeo BUILDTAGS="$BUILDTAGS"
    make install PREFIX=/usr/local
}

_run_cross() {
    make local-cross BUILDTAGS="$BUILDTAGS"
}

_run_doccheck() {
    make validate-docs BUILDTAGS="$BUILDTAGS"
}

_run_unit() {
    make test-unit-local BUILDTAGS="$BUILDTAGS"
}

_podman_reset() {
    # Ensure we start with a clean-slate
    showrun podman system reset --force
}

_run_integration() {
    _podman_reset
    make test-integration-local BUILDTAGS="$BUILDTAGS"
}

_run_system() {
    _podman_reset
    ##### Note: Test MODIFIES THE HOST SETUP #####
    make test-system-local BUILDTAGS="$BUILDTAGS"
}

req_env_vars SKOPEO_PATH

handler="_run_${1}"
if [ "$(type -t $handler)" != "function" ]; then
    die "Unknown/Unsupported command-line argument '$1'"
fi

msg "************************************************************"
msg "Runner executing $1 on $OS_REL_VER"
msg "************************************************************"

cd "$SKOPEO_PATH"
$handler
