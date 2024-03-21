#!/usr/bin/env bash
tmpdir="$PWD/tmp.$RANDOM"
mkdir -p "$tmpdir"
trap 'rm -fr "$tmpdir"' EXIT
cc -c -o "$tmpdir"/libdm_tag.o -x c - > /dev/null 2> /dev/null << EOF
#include <libdevmapper.h>
int main() {
	struct dm_task *task;
	return 0;
}
EOF
if test $? -ne 0 ; then
	echo "libdm_no_deferred_remove exclude_graphdriver_devicemapper"
fi
