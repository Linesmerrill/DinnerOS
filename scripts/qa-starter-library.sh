#!/bin/bash
# Makes a small starter library on a local API for UI tests that need one (ReviewPathQATests),
# and prints its household ID. Run it once against a local API with developer sign-in on, then
# start the API that should copy it into new households with STARTER_RECIPES_HOUSEHOLD_ID=<id>.
# Local only: it refuses any address that isn't localhost.
#   scripts/qa-starter-library.sh http://localhost:8082
set -euo pipefail
BASE=${1:-http://localhost:8082}
case $BASE in http://localhost:* | http://127.0.0.1:*) ;; *) echo "local APIs only" >&2; exit 1 ;; esac

token=$(curl -sf -X POST "$BASE/api/v1/auth/dev" -H 'Content-Type: application/json' \
    -d '{"subject":"qa-starter-library","email":"starter@example.test"}' |
    python3 -c 'import json,sys; print(json.load(sys.stdin)["accessToken"])')
auth=(-H "Authorization: Bearer $token" -H 'Content-Type: application/json')

hid=$(curl -sf "$BASE/api/v1/households" "${auth[@]}" |
    python3 -c 'import json,sys
items=json.load(sys.stdin).get("items") or []
print(items[0]["household"]["id"] if items else "")')
if [ -z "$hid" ]; then
    hid=$(curl -sf -X POST "$BASE/api/v1/households" "${auth[@]}" \
        -d '{"name":"Starter Library","timeZone":"America/Phoenix","defaultServings":2}' |
        python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("id") or d["household"]["id"])')
fi

count=$(curl -sf "$BASE/api/v1/households/$hid/recipes" "${auth[@]}" |
    python3 -c 'import json,sys
print(len(json.load(sys.stdin).get("items") or []))')
if [ "$count" -lt 3 ]; then
    for text in \
        $'Lemon Garlic Chicken\nServes 2\n30 minutes\nIngredients\n2 chicken breasts\n1 lemon\n3 cloves garlic\n2 Tbsp olive oil\nSteps\nSeason the chicken with salt and pepper.\nSear in the oil for 6 minutes a side.\nAdd the garlic and lemon juice and cook 1 minute more.' \
        $'Black Bean Tacos\nServes 2\n20 minutes\nIngredients\n1 can black beans\n6 corn tortillas\n1 avocado\n1 lime\nSteps\nWarm the beans with a pinch of cumin.\nWarm the tortillas in a dry pan.\nFill with beans, sliced avocado and a squeeze of lime.' \
        $'Tomato Basil Pasta\nServes 2\n25 minutes\nIngredients\n8 oz spaghetti\n1 pint cherry tomatoes\n2 Tbsp olive oil\n1 handful basil\nSteps\nBoil the pasta until tender.\nCook the tomatoes in the oil until they burst, about 8 minutes.\nToss with the pasta and torn basil.'; do
        draft=$(curl -sf -X POST "$BASE/api/v1/households/$hid/recipes/parse" "${auth[@]}" \
            -d "$(python3 -c 'import json,sys; print(json.dumps({"text": sys.argv[1]}))' "$text")")
        curl -sf -X POST "$BASE/api/v1/households/$hid/recipes" "${auth[@]}" -d "$draft" >/dev/null
    done
fi
echo "$hid"
