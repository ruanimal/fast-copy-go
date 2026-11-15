#!/usr/bin/env bash
set -euo pipefail

case "${1:?expected linux or darwin}" in
  linux) patterns=('*.deb' '*.rpm' '*.tar.gz') ;;
  darwin) patterns=('*.dmg') ;;
  *) echo "Unknown platform: $1" >&2; exit 1 ;;
esac

for arch in amd64 arm64; do
  for pattern in "${patterns[@]}"; do
    if ! compgen -G "dist/$1-$arch/$pattern" > /dev/null; then
      echo "Missing $1/$arch package: $pattern" >&2
      exit 1
    fi
  done
done
