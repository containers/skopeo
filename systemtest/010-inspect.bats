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

    # Each SHA-named file must be listed in the output of 'inspect'
    for sha in $(find $workdir -type f | xargs -l1 basename | egrep '^[0-9a-f]{64}$'); do
        expect_output --from="$inspect_local" --substring "sha256:$sha" \
                      "Locally-extracted SHA file is present in 'inspect'"
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
    while IFS=$' \t\n' read key expect; do
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
    remote_image=docker://docker.io/fedora:latest
    run_skopeo inspect $remote_image
    inspect_remote=$output

    # Simple check on 'inspect' output with environment variables.
    #    1) Get remote image values of environment variables (the value of 'Env')
    #    2) Confirm substring in check_array and the value of 'Env' match.
    check_array=(PATH=.* )
    remote=$(jq '.Env[]' <<<"$inspect_remote")
    for substr in ${check_array[@]}; do
        expect_output --from="$remote" --substring "$substr"
    done
}

@test "inspect: image manifest list w/ diff platform" {
    # When --raw is provided, can inspect show the raw manifest list, w/o
    # requiring any particular platform to be present
    # To test whether container image can be inspected successfully w/o
    # platform dependency.
    #    1) Get current platform arch
    #    2) Inspect container image is different from current platform arch
    #    3) Compare output w/ expected result

    # Here we see a revolting workaround for a podman incompatibility
    # change: in April 2020, podman info completely changed format
    # of the keys. What worked until then now throws an error. We
    # need to work with both old and new podman.
    arch=$(podman info --format '{{.host.arch}}' || true)
    if [[ -z "$arch" ]]; then
        arch=$(podman info --format '{{.Host.Arch}}')
    fi

    case $arch in
        "amd64")
            diff_arch_list="s390x ppc64le"
            ;;
        "s390x")
            diff_arch_list="amd64 ppc64le"
            ;;
        "ppc64le")
            diff_arch_list="amd64 s390x"
            ;;
        "*")
            diff_arch_list="amd64 s390x ppc64le"
            ;;
    esac

    for arch in $diff_arch_list; do
        remote_image=docker://docker.io/$arch/golang
        run_skopeo inspect --tls-verify=false --raw $remote_image
        remote_arch=$(jq -r '.manifests[0]["platform"]["architecture"]' <<< "$output")
        expect_output --from="$remote_arch" "$arch" "platform arch of $remote_image"
    done
}

# vim: filetype=sh
