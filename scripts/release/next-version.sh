#!/bin/sh
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

# Prints the release version after LATEST for a BUMP of major, minor or patch.
#
#   next-version.sh v1.4.2 minor   # v1.5.0
#   next-version.sh "" patch       # v0.1.0 (the first release)
#
# With no previous release, the first version is v0.1.0, or v1.0.0 for a
# major bump.
set -eu

latest=${1-}
bump=${2:?usage: next-version.sh LATEST-TAG major|minor|patch}

case $bump in
major | minor | patch) ;;
*)
	echo "unknown bump \"$bump\" (want major, minor or patch)" >&2
	exit 2
	;;
esac

if [ -z "$latest" ]; then
	if [ "$bump" = major ]; then echo v1.0.0; else echo v0.1.0; fi
	exit 0
fi

version=${latest#v}
case $version in
*[!0-9.]* | *..* | .* | *.)
	echo "latest tag \"$latest\" is not vMAJOR.MINOR.PATCH" >&2
	exit 2
	;;
esac
IFS=. read -r major minor patch extra <<EOF
$version
EOF
if [ -z "${patch-}" ] || [ -n "${extra-}" ]; then
	echo "latest tag \"$latest\" is not vMAJOR.MINOR.PATCH" >&2
	exit 2
fi

case $bump in
major) echo "v$((major + 1)).0.0" ;;
minor) echo "v$major.$((minor + 1)).0" ;;
patch) echo "v$major.$minor.$((patch + 1))" ;;
esac
