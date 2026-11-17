#!/bin/bash
# Stock the invite free month code pool from an App Store Connect download (decision 755).
#
#   OPERATOR_TOKEN=<access token> scripts/offer-codes.sh import codes.csv "Invite free month" 2026-11-a 2027-02-15
#   OPERATOR_TOKEN=<access token> scripts/offer-codes.sh stock
#
# OPERATOR_TOKEN is the access token of a user in OPERATOR_USER_IDS (sign in to the app, or the
# web sign-in, as that person). API_BASE defaults to https://api.tlps.dev. The codes go straight
# from the file to the API: this prints counts only, never a code. Delete the CSV once it's in.
set -euo pipefail
API_BASE=${API_BASE:-https://api.tlps.dev}
: "${OPERATOR_TOKEN:?Set OPERATOR_TOKEN to the access token of an operator}"

usage() {
    sed -n '2,8p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
}

urlencode() { python3 -c 'import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1], safe=""))' "$1"; }

case ${1:-} in
import)
    [ $# -eq 5 ] || usage
    file=$2 offer=$3 batch=$4 expires=$5
    [ -f "$file" ] || { echo "No file at $file"; exit 1; }
    curl -fsS -X POST \
        "$API_BASE/api/v1/admin/plus/offer-codes?offer=$(urlencode "$offer")&batch=$(urlencode "$batch")&expiresAt=$(urlencode "$expires")" \
        -H "Authorization: Bearer $OPERATOR_TOKEN" -H 'Content-Type: text/csv' --data-binary "@$file" |
        python3 -m json.tool
    ;;
stock)
    curl -fsS "$API_BASE/api/v1/admin/plus/offer-codes/stock" -H "Authorization: Bearer $OPERATOR_TOKEN" |
        python3 -m json.tool
    ;;
*) usage ;;
esac
