#!/bin/sh
# Cross-compiles brewdust VERSION into OUTDIR as one .tar.gz per platform,
# plus a checksums.txt.
#
#   scripts/release/build.sh v0.1.0 dist
set -eu

version=${1:?usage: build.sh VERSION OUTDIR}
out=${2:?usage: build.sh VERSION OUTDIR}
platforms=${PLATFORMS:-darwin/arm64 darwin/amd64 linux/arm64 linux/amd64}

root=$(cd "$(dirname "$0")/../.." && pwd)
rm -rf "$out"
mkdir -p "$out"
out=$(cd "$out" && pwd)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

cd "$root"
for platform in $platforms; do
	os=${platform%/*}
	arch=${platform#*/}
	name="brewdust_${version#v}_${os}_${arch}"
	mkdir "$stage/$name"
	echo "building $name"
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath \
		-ldflags "-s -w -X main.version=$version" \
		-o "$stage/$name/brewdust" .
	cp README.md "$stage/$name/"
	tar -C "$stage" -czf "$out/$name.tar.gz" "$name"
done

cd "$out"
if command -v sha256sum >/dev/null; then
	sha256sum ./*.tar.gz | sed 's| \./| |' >checksums.txt
else
	shasum -a 256 ./*.tar.gz | sed 's| \./| |' >checksums.txt
fi
cat checksums.txt
