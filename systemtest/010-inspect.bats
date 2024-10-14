#!/usr/bin/env bats
#
# Simplest test for skopeo inspect
#

load helpers

@test "inspect: basic" {
    workdir=$TESTDIR/inspect

    remote_image=docker://quay.io/libpod/alpine_labels:latest
    # Inspect remote source, then pull it. There's a small race condition
    # in which the remote image can get updated between the inspect and
    # the copy; let's just not worry about it.
    run_skopeo inspect $remote_image
    inspect_remote=$output

    # Now pull it into a directory
    run_skopeo copy $remote_image dir:$workdir
    expect_output --substring "Getting image source signatures"
    expect_output --substring "Writing manifest to image destination"

    # Unpacked contents must include a manifest and version
    [ -e $workdir/manifest.json ]
    [ -e $workdir/version ]

    # Now run inspect locally
    run_skopeo inspect dir:$workdir
    inspect_local=$output
    run_skopeo inspect --raw dir:$workdir
    inspect_local_raw=$output
    config_digest=$(jq -r '.config.digest' <<<"$inspect_local_raw")

    # Each SHA-named layer file (but not the config) must be listed in the output of 'inspect'.
    # In all existing versions of Skopeo (with 1.6 being the current as of this comment),
    # the output of 'inspect' lists layer digests,
    # but not the digest of the config blob ($config_digest), if any.
    layers=$(jq -r '.Layers' <<<"$inspect_local")
    for sha in $(find $workdir -type f | xargs -l1 basename | grep -E '^[0-9a-f]{64}$'); do
        if [ "sha256:$sha" != "$config_digest" ]; then
            expect_output --from="$layers" --substring "sha256:$sha" \
                        "Locally-extracted SHA file is present in 'inspect'"
        fi
    done

    # Simple sanity check on 'inspect' output.
    # For each of the given keys (LHS of the table below):
    #    1) Get local and remote values
    #    2) Sanity-check local value using simple expression
    #    3) Confirm that local and remote values match.
    #
    # The reason for (2) is to make sure that we don't compare bad results
    #
    # The reason for a hardcoded list, instead of 'jq keys', is that RepoTags
    # is always empty locally, but a list remotely.
    while read key expect; do
        local=$(echo "$inspect_local" | jq -r ".$key")
        remote=$(echo "$inspect_remote" | jq -r ".$key")

        expect_output --from="$local" --substring "$expect" \
                  "local $key is sane"

        expect_output --from="$remote" "$local" \
                      "local $key matches remote"
    done <<END_EXPECT
Architecture       amd64
Created            [0-9-]+T[0-9:]+\.[0-9]+Z
Digest             sha256:[0-9a-f]{64}
DockerVersion      [0-9]+\.[0-9][0-9.-]+
Labels             \\\{.*PODMAN.*podman.*\\\}
Layers             \\\[.*sha256:.*\\\]
Os                 linux
END_EXPECT
}

@test "inspect: env" {
    remote_image=docker://quay.io/libpod/fedora:31
    run_skopeo inspect $remote_image
    inspect_remote=$output

    # Simple check on 'inspect' output with environment variables.
    #    1) Get remote image values of environment variables (the value of 'Env')
    #    2) Confirm substring in check_array and the value of 'Env' match.
    check_array=(FGC=f31 DISTTAG=f31container)
    remote=$(jq '.Env[]' <<<"$inspect_remote")
    for substr in ${check_array[@]}; do
        expect_output --from="$remote" --substring "$substr"
    done
}

# Tests https://github.com/containers/skopeo/pull/708
@test "inspect: image manifest list w/ diff platform" {
    # This image's manifest is for an os + arch that is... um, unlikely
    # to support skopeo in the foreseeable future. Or past. The image
    # is created by the make-noarch-manifest script in this directory.
    img=docker://quay.io/libpod/notmyarch:20210121

    # Get our host golang arch (what we're running on, according to golang).
    # This assumes that skopeo arch matches host arch (which it always should).
    # Buildah is used here because it depends less on the exact system config
    # than podman - and all we're really after is the golang-flavored arch name.
    arch=$(go env GOARCH)

    # By default, 'inspect' tries to match our host os+arch. This should fail.
    run_skopeo 1 inspect $img
    expect_output --substring "parsing manifest for image: choosing image instance: no image found in manifest list for architecture \\\\\"$arch\\\\\", variant " \
                  "skopeo inspect, without --raw, fails"

    # With --raw, we can inspect
    run_skopeo inspect --raw $img
    expect_output --substring "manifests.*platform.*architecture" \
                  "skopeo inspect --raw returns reasonable output"

    # ...and what we get should be consistent with what our script created.
    archinfo=$(jq -r '.manifests[0].platform | {os,variant,architecture} | join("-")' <<<"$output")

    expect_output --from="$archinfo" "amigaos-1000-mc68000" \
                  "os - variant - architecture of $img"
}

@test "inspect: don't list tags" {
    remote_image=docker://quay.io/fedora/fedora
    # use --no-tags to not list any tags
    run_skopeo inspect --no-tags $remote_image
    inspect_output=$output
    # extract the content of "RepoTags" property from the JSON output
    repo_tags=$(jq '.RepoTags[]' <<<"$inspect_output")
    # verify that the RepoTags was empty
    expect_output --from="$repo_tags" "" "inspect --no-tags was expected to return empty RepoTags[]"
}

@test "inspect: image unknown" {
    # non existing image
    run_skopeo 2 inspect containers-storage:non-existing-tag
    expect_output --substring "does not resolve to an image ID" \
		  "skopeo inspect containers-storage:010101010101"
}

# vim: filetype=sh
