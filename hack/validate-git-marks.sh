#!/usr/bin/env bash

IFS=$'\n'
files=( $(git ls-tree -r HEAD --name-only | grep -v '^vendor/' || true) )
unset IFS

badFiles=()
for f in "${files[@]}"; do
    if grep -Eq '^(<<<<<<<|>>>>>>>|=======$)' "$f"; then
        badFiles+=( "$f" )
    fi
done


if [ ${#badFiles[@]} -eq 0 ]; then
	echo 'Congratulations!  There is no conflict.'
else
	{
		echo "There is trace of conflict(s) in the following files :"
		for f in "${badFiles[@]}"; do
			echo " - $f"
		done
		echo
		echo 'Please fix the conflict(s) commit the result.'
		echo
	} >&2
	exit 1
fi
