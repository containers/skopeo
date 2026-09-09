#!/usr/bin/env bats
#
# Sync tests
#

load helpers

function setup() {
    standard_setup
}

@test "sync: --dry-run" {
    local remote_image=quay.io/libpod/busybox:latest
    local dir=$TESTDIR/dir

    run_skopeo sync --dry-run --src docker --dest dir --scoped $remote_image $dir
    expect_output --substring "Would have copied image"
    expect_output --substring "from=docker://${remote_image} to=dir:${dir}/${remote_image}"
    expect_output --substring "Would have synced 1 images from 1 sources"
}

teardown() {
    standard_teardown
}

# vim: filetype=sh
