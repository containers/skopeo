#!/usr/bin/env bash

set -exo pipefail

cat /etc/os-release

cd ../../..
dnf builddep -y rpm/skopeo.spec
make
make install

EXT_REPO_NAME=ostree-rs-ext
EXT_REPO_HOME=$(mktemp -d)/$EXT_REPO_NAME
EXT_REPO=https://github.com/ostreedev/${EXT_REPO_NAME}.git
git clone --depth 1 $EXT_REPO $EXT_REPO_HOME
cd $EXT_REPO_HOME
cargo test --no-run
cargo test -- --nocapture --quiet
