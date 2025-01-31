#!/usr/bin/env bash

set -exo pipefail

uname -r

rpm -q \
    bats \
    containers-common \
    skopeo \
    skopeo-tests \

bats /usr/share/skopeo/test/system
