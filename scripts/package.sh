#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
version=${1:-dev}
case "$version" in
  ''|*[!a-zA-Z0-9.+-]*) echo "Invalid release version" >&2; exit 1 ;;
esac
mkdir -p dist
staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT HUP INT TERM
cp LICENSE README.md "$staging/"
for platform in darwin linux; do
  for architecture in amd64 arm64; do
    cp "bin/ferretta-$platform-$architecture" "$staging/ferretta"
    tar -czf "dist/ferretta-$version-$platform-$architecture.tar.gz" -C "$staging" ferretta LICENSE README.md
  done
done
cd dist
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "ferretta-$version-"*.tar.gz > "ferretta-$version-checksums.txt"
else
  shasum -a 256 "ferretta-$version-"*.tar.gz > "ferretta-$version-checksums.txt"
fi
