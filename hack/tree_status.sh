#!/usr/bin/env bash
set -e

# TMT breaks this so we only check go.* and vendor
# https://github.com/teemtee/tmt/issues/3800
# STATUS=$(git status --porcelain)
STATUS=$(git status --porcelain go.* vendor)
if [[ -z $STATUS ]]
then
	echo "tree is clean"
else
	echo "tree is dirty, please commit all changes and sync the vendor.conf"
	echo ""
	echo "$STATUS"
	exit 1
fi
