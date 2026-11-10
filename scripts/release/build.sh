#!/bin/sh
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

# Cross-compiles brewdust VERSION into OUTDIR as one .tar.gz per platform,
# plus a checksums.txt.
#
#   scripts/release/build.sh v0.1.0 dist
#
# macOS binaries are signed when SIGN_IDENTITY names a Developer ID
# Application certificate in the keychain, and then notarized when
# notarytool credentials are given, either as a stored profile:
#
#   NOTARY_PROFILE=brewdust
#
# or as an App Store Connect API key (as in CI):
#
#   NOTARY_KEY=AuthKey_XXXXXXXXXX.p8 NOTARY_KEY_ID=XXXXXXXXXX NOTARY_ISSUER=<uuid>
#
# Set REQUIRE_SIGNING=1 to fail rather than build unsigned macOS binaries.
set -eu

version=${1:?usage: build.sh VERSION OUTDIR}
out=${2:?usage: build.sh VERSION OUTDIR}
platforms=${PLATFORMS:-darwin/arm64 darwin/amd64 linux/arm64 linux/amd64}
identifier=com.boxingoctopuscreative.brewdust

root=$(cd "$(dirname "$0")/../.." && pwd)
rm -rf "$out"
mkdir -p "$out"
out=$(cd "$out" && pwd)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

notary_auth() {
	if [ -n "${NOTARY_PROFILE-}" ]; then
		echo "--keychain-profile $NOTARY_PROFILE"
	elif [ -n "${NOTARY_KEY-}" ]; then
		echo "--key $NOTARY_KEY --key-id ${NOTARY_KEY_ID:?NOTARY_KEY_ID is required with NOTARY_KEY} --issuer ${NOTARY_ISSUER:?NOTARY_ISSUER is required with NOTARY_KEY}"
	fi
}

cd "$root"
names=""
macs=""
for platform in $platforms; do
	os=${platform%/*}
	arch=${platform#*/}
	name="brewdust_${version#v}_${os}_${arch}"
	names="$names $name"
	mkdir "$stage/$name"
	echo "building $name"
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath \
		-ldflags "-s -w -X main.version=$version" \
		-o "$stage/$name/brewdust" .
	cp README.md LICENSE "$stage/$name/"
	[ "$os" = darwin ] && macs="$macs $name"
done

if [ -n "$macs" ]; then
	if [ -z "${SIGN_IDENTITY-}" ] || [ -z "$(notary_auth)" ]; then
		if [ "${REQUIRE_SIGNING-}" = 1 ]; then
			echo "REQUIRE_SIGNING is set, but SIGN_IDENTITY or notarytool credentials are missing" >&2
			exit 1
		fi
		echo "warning: macOS binaries are not signed and notarized (see the comment at the top of $0)" >&2
	else
		for name in $macs; do
			echo "signing $name"
			codesign --sign "$SIGN_IDENTITY" --identifier "$identifier" \
				--options runtime --timestamp --force "$stage/$name/brewdust"
			codesign --verify --strict "$stage/$name/brewdust"
		done

		# One submission covers every macOS binary.
		echo "notarizing (this usually takes a minute or two)"
		(cd "$stage" && zip -qr notarize.zip $(for n in $macs; do echo "$n/brewdust"; done))
		# shellcheck disable=SC2046 # notary_auth is meant to split into arguments
		result=$(xcrun notarytool submit "$stage/notarize.zip" $(notary_auth) \
			--wait --timeout 30m --output-format json)
		status=$(echo "$result" | sed -n 's/.*"status" *: *"\([^"]*\)".*/\1/p')
		id=$(echo "$result" | sed -n 's/.*"id" *: *"\([^"]*\)".*/\1/p')
		if [ "$status" != Accepted ]; then
			echo "notarization failed ($status): $result" >&2
			# shellcheck disable=SC2046
			[ -n "$id" ] && xcrun notarytool log "$id" $(notary_auth) >&2
			exit 1
		fi
		echo "notarized: submission $id accepted"
	fi
fi

for name in $names; do
	tar -C "$stage" -czf "$out/$name.tar.gz" "$name"
done

cd "$out"
if command -v sha256sum >/dev/null; then
	sha256sum ./*.tar.gz | sed 's| \./| |' >checksums.txt
else
	shasum -a 256 ./*.tar.gz | sed 's| \./| |' >checksums.txt
fi
cat checksums.txt
