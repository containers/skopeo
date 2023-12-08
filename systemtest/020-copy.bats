#!/usr/bin/env bats
#
# Copy tests
#

load helpers

function setup() {
    standard_setup

    start_registry reg
}

# From remote, to dir1, to local, to dir2;
# compare dir1 and dir2, expect no changes
@test "copy: dir, round trip" {
    local remote_image=docker://quay.io/libpod/busybox:latest
    local localimg=docker://localhost:5000/busybox:unsigned

    local dir1=$TESTDIR/dir1
    local dir2=$TESTDIR/dir2

    run_skopeo copy          $remote_image  dir:$dir1
    run_skopeo copy --dest-tls-verify=false dir:$dir1  $localimg
    run_skopeo copy  --src-tls-verify=false            $localimg  dir:$dir2

    # Both extracted copies must be identical
    diff -urN $dir1 $dir2
}

# Same as above, but using 'oci:' instead of 'dir:' and with a :latest tag
@test "copy: oci, round trip" {
    local remote_image=docker://quay.io/libpod/busybox:latest
    local localimg=docker://localhost:5000/busybox:unsigned

    local dir1=$TESTDIR/oci1
    local dir2=$TESTDIR/oci2

    run_skopeo copy          $remote_image  oci:$dir1:latest
    run_skopeo copy --dest-tls-verify=false oci:$dir1:latest  $localimg
    run_skopeo copy  --src-tls-verify=false                   $localimg  oci:$dir2:latest

    # Both extracted copies must be identical
    diff -urN $dir1 $dir2
}

# Compression zstd
@test "copy: oci, zstd" {
    local remote_image=docker://quay.io/libpod/busybox:latest

    local dir=$TESTDIR/dir

    run_skopeo copy --dest-compress-format=zstd $remote_image oci:$dir:latest

    # zstd magic number
    local magic=$(printf "\x28\xb5\x2f\xfd")

    # Check there is at least one file that has the zstd magic number as the first 4 bytes
    (for i in $dir/blobs/sha256/*; do test "$(head -c 4 $i)" = $magic && exit 0; done; exit 1)

    # Check that the manifest's description of the image's first layer is the zstd layer type
    instance=$(jq -r '.manifests[0].digest' $dir/index.json)
    [[ "$instance" != null ]]
    mediatype=$(jq -r '.layers[0].mediaType' < $dir/blobs/${instance/://})
    [[ "$mediatype" == "application/vnd.oci.image.layer.v1.tar+zstd" ]]
}

# Same image, extracted once with :tag and once without
@test "copy: oci w/ and w/o tags" {
    local remote_image=docker://quay.io/libpod/busybox:latest

    local dir1=$TESTDIR/dir1
    local dir2=$TESTDIR/dir2

    run_skopeo copy $remote_image oci:$dir1
    run_skopeo copy $remote_image oci:$dir2:withtag

    # Both extracted copies must be identical, except for index.json
    diff -urN --exclude=index.json $dir1 $dir2

    # ...which should differ only in the tag. (But that's too hard to check)
    grep '"org.opencontainers.image.ref.name":"withtag"' $dir2/index.json
}

# Registry -> storage -> oci-archive
@test "copy: registry -> storage -> oci-archive" {
    local alpine=quay.io/libpod/alpine:latest
    local tmp=$TESTDIR/oci

    run_skopeo copy docker://$alpine containers-storage:$alpine
    run_skopeo copy containers-storage:$alpine oci-archive:$tmp
}

# This one seems unlikely to get fixed
@test "copy: bug 651" {
    skip "Enable this once skopeo issue #651 has been fixed"

    run_skopeo copy --dest-tls-verify=false \
               docker://quay.io/libpod/alpine_labels:latest \
               docker://localhost:5000/foo
}

# manifest format
@test "copy: manifest format" {
    local remote_image=docker://quay.io/libpod/busybox:latest

    local dir1=$TESTDIR/dir1
    local dir2=$TESTDIR/dir2

    run_skopeo copy --format v2s2 $remote_image dir:$dir1
    run_skopeo copy --format oci $remote_image dir:$dir2
    grep 'application/vnd.docker.distribution.manifest.v2' $dir1/manifest.json
    grep 'application/vnd.oci.image' $dir2/manifest.json
}

# additional tag
@test "copy: additional tag" {
    local remote_image=docker://quay.io/libpod/busybox:latest

    # additional-tag is supported only for docker-archive
    run_skopeo copy --additional-tag busybox:mine $remote_image \
               docker-archive:$TESTDIR/mybusybox.tar:busybox:latest
    mkdir -p $TESTDIR/podmanroot
    run podman --root $TESTDIR/podmanroot load -i $TESTDIR/mybusybox.tar
    run podman --root $TESTDIR/podmanroot images
    expect_output --substring "mine"

    # rootless cleanup needs to be done with unshare due to subuids
    if [[ "$(id -u)" != "0" ]]; then
        run podman unshare rm -rf $TESTDIR/podmanroot
    fi
}

# shared blob directory
@test "copy: shared blob directory" {
    local remote_image=docker://quay.io/libpod/busybox:latest

    local shareddir=$TESTDIR/shareddir
    local dir1=$TESTDIR/dir1
    local dir2=$TESTDIR/dir2

    run_skopeo copy --dest-shared-blob-dir $shareddir \
               $remote_image oci:$dir1
    [ -n "$(ls $shareddir)" ]
    [ -z "$(ls $dir1/blobs)" ]
    run_skopeo copy --src-shared-blob-dir $shareddir \
               oci:$dir1 oci:$dir2
    diff -urN $shareddir $dir2/blobs
}

@test "copy: sif image" {
    type -path fakeroot || skip "'fakeroot' tool not available"

    local localimg=dir:$TESTDIR/dir

    run_skopeo copy sif:${TEST_SOURCE_DIR}/testdata/busybox_latest.sif $localimg
    run_skopeo inspect $localimg --format "{{.Architecture}}"
    expect_output "amd64"
}

@test "copy: --dry-run" {
    local remote_image=docker://quay.io/libpod/busybox:latest
    local dir=$TESTDIR/dir

    run_skopeo copy --dry-run $remote_image oci:$dir:latest
    expect_output --substring "Running in dry-run mode"
    expect_output --substring "Would have copied from=${remote_image} to=oci:${dir}:latest"
}

teardown() {
    podman rm -f reg

    standard_teardown
}

# vim: filetype=sh
