#!/bin/bash
# Fails if the decision log in docs/architecture.md reuses a row number.
#
# Parallel branches each append a row, and two branches picking the same number
# conflict on merge every time. This catches it before the merge instead.
set -euo pipefail

doc="${1:-docs/architecture.md}"

duplicates=$(grep -oE '^\| [0-9]+ \|' "$doc" | grep -oE '[0-9]+' | sort -n | uniq -d)

if [ -n "$duplicates" ]; then
	echo "Duplicate decision row numbers in $doc:" >&2
	for n in $duplicates; do
		echo "  row $n:" >&2
		grep -nE "^\| $n \|" "$doc" | cut -c1-120 | sed 's/^/    /' >&2
	done
	echo >&2
	echo "Give each decision its own number; the highest in use is $(grep -oE '^\| [0-9]+ \|' "$doc" | grep -oE '[0-9]+' | sort -n | tail -1)." >&2
	exit 1
fi

echo "decision rows: no duplicates ($(grep -cE '^\| [0-9]+ \|' "$doc") rows, highest $(grep -oE '^\| [0-9]+ \|' "$doc" | grep -oE '[0-9]+' | sort -n | tail -1))"
