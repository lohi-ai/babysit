#!/usr/bin/env bash
#
# Build the dashboard SPA and stage it where //go:embed can see it
# (internal/webui/dist). Run by .goreleaser.yaml's before-hooks, so every
# released binary carries a dashboard and `brew install bbs && bbs dashboard`
# works with no checkout and no npm.
#
# Not part of `go build`: a normal build leaves the placeholder in place and
# `bbs dashboard` falls back to web/dist on disk.
set -euo pipefail

cd "$(dirname "$0")/.."
dest=internal/webui/dist

npm --prefix web ci
npm --prefix web run build

find "$dest" -mindepth 1 ! -name .keep -delete
cp -R web/dist/. "$dest"/

# data.js is deliberately not shipped: it is a snapshot of whoever ran the
# build's local ticket state (~1 MB of it), and baking that into a public
# binary would publish it. The served dashboard reads the JSON API, and the
# snapshot path writes its own data.js next to the unpacked copy.
rm -f "$dest/data.js"

test -f "$dest/index.html" || { echo "build-webui: no index.html in $dest" >&2; exit 1; }
echo "build-webui: staged $(du -sh "$dest" | cut -f1) into $dest"
