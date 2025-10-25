#!/bin/bash
set -e

# These tests can run in/outside of a container.  However,
# not all storage drivers are supported in a container
# environment.  Detect this and setup storage when
# running in a container.
#
# Paradoxically (FIXME: clean this up), SKOPEO_CONTAINER_TESTS is set
# both inside a container and without a container (in a CI VM); it actually means
# "it is safe to destructively modify the system for tests".
#
# On a CI VM, we can just use Podman as it is already configured; the changes below,
# to use VFS, are necessary only inside a container, because overlay-inside-overlay
# does not work. So, make these changes conditional on both
# SKOPEO_CONTAINER_TESTS (for acceptability to do destructive modification) and !CI
# (for necessity to adjust for in-container operation)
if ((SKOPEO_CONTAINER_TESTS)) && [[ "$CI" != true ]]; then
    if [[ -r /etc/containers/storage.conf ]]; then
        echo "MODIFYING existing storage.conf"
        sed -i \
            -e 's/^driver\s*=.*/driver = "vfs"/' \
            -e 's/^mountopt/#mountopt/' \
            /etc/containers/storage.conf
    else
        echo "CREATING NEW storage.conf"
        cat >> /etc/containers/storage.conf << EOF
[storage]
driver = "vfs"
runroot = "/run/containers/storage"
graphroot = "/var/lib/containers/storage"
EOF
    fi
    # The logic of finding the relevant storage.conf file is convoluted
    # and in effect differs between Skopeo and Podman, at least in some versions;
    # explicitly point at the file we want to use to hopefully avoid that.
    export CONTAINERS_STORAGE_CONF=/etc/containers/storage.conf
fi

SKOPEO_BINARY="$SKOPEO_BINARY" bats --tap systemtest
