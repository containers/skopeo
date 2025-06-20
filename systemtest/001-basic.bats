#!/usr/bin/env bats
#
# Simplest set of skopeo tests. If any of these fail, we have serious problems.
#

load helpers

# Override standard setup! We don't yet trust anything
function setup() {
    :
}

@test "skopeo version emits reasonable output" {
    run_skopeo --version

    expect_output --substring "skopeo version [0-9.]+"
}

@test "skopeo release isn't a development version" {
    [[ "${RELEASE_TESTING:-false}" == "true" ]] || \
      skip "Release testing may be enabled by setting \$RELEASE_TESTING = 'true'."

    run_skopeo --version

    # expect_output() doesn't support negative matching
    if [[ "$output" =~ "dev" ]]; then
        # This is a multi-line message, which may in turn contain multi-line
        # output, so let's format it ourselves, readably
        local -a output_split
        readarray -t output_split <<<"$output"
        printf "#/vvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvv\n"  >&2
        printf "#|       FAIL: $BATS_TEST_NAME\n"                   >&2
        printf "#| unexpected: 'dev'\n"                             >&2
        printf "#|     actual: '%s'\n" "${output_split[0]}"         >&2
        local line
        for line in "${output_split[@]:1}"; do
            printf "#|           > '%s'\n" "$line"                  >&2
        done
        printf "#\\^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^\n" >&2
        false
    fi
}


# vim: filetype=sh
