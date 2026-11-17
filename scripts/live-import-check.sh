#!/bin/bash
# Reads real recipe links the way the server does, over the network. Not run in CI.
#
#   scripts/live-import-check.sh                       # the Fry's link from build 115
#   scripts/live-import-check.sh URL [URL...]          # any links
#   LIVE_HTML=/path/to/saved.html scripts/live-import-check.sh   # also read a saved, rendered page
#
# See docs/development.md, "Checking recipe links on real sites".
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
DINNEROS_LIVE_IMPORT=1 DINNEROS_LIVE_IMPORT_URLS="$*" DINNEROS_LIVE_IMPORT_HTML="${LIVE_HTML:-}" \
	go -C "$ROOT/api" test -count=1 -v -run '^TestLiveImport$' ./internal/recipes
