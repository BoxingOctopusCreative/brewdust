#!/bin/sh
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

# Reads a pull request's label names, one per line, and prints the version
# bump they ask for: major, minor, patch or none. Without a release:* label
# it's patch; more than one, or an unknown one, is an error.
#
#   printf 'bug\nrelease:minor\n' | bump-from-labels.sh   # minor
set -eu

labels=$(grep '^release:' || true)
count=$(printf '%s' "$labels" | grep -c . || true)

if [ "$count" -eq 0 ]; then
	echo patch
	exit 0
fi
if [ "$count" -gt 1 ]; then
	echo "more than one release label: $(echo "$labels" | tr '\n' ' ')" >&2
	exit 1
fi
bump=${labels#release:}
case $bump in
major | minor | patch | none) echo "$bump" ;;
*)
	echo "unknown label release:$bump (want release:major, minor, patch or none)" >&2
	exit 1
	;;
esac
