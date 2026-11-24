#!/usr/bin/env bash
# Builds cmd/printty/rsrc_windows_*.syso: printty.exe's icon and version
# details (Windows shows "Printty" in notifications and Explorer, not
# "printty.exe"). go build picks the .syso files up by itself.
#   tools/winres.sh 0.1.0
# go-winres is a build tool run with "go run": it is not a module dependency.
set -euo pipefail
version="${1:-0.0.0}"
cd "$(dirname "$0")/.."
go run ./tools/mkicon
go run github.com/tc-hib/go-winres@v0.3.3 simply \
  --arch amd64,arm64 --out cmd/printty/rsrc --manifest cli \
  --icon packaging/windows/printty.ico \
  --product-name Printty --file-description Printty \
  --product-version "$version" --file-version "$version" \
  --original-filename printty.exe \
  --copyright "TriptoAfsin"
