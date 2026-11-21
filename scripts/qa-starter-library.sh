#!/bin/bash
# Makes a small starter library on a local API for UI tests that need one (ReviewPathQATests),
# and prints its household ID. Run it once against a local API with developer sign-in on, then
# start the API that should copy it into new households with STARTER_RECIPES_HOUSEHOLD_ID=<id>.
# Local only: it refuses any address that isn't localhost.
#   scripts/qa-starter-library.sh http://localhost:8082
#
# With "pantry" it instead stocks the main QA household's pantry (the dev-simulator sign-in the
# screenshot tests use) with a realistic 14 items across the pantry, fridge and freezer, when that
# pantry is empty. Re-running it adds nothing.
#   scripts/qa-starter-library.sh http://localhost:8082 pantry
set -euo pipefail
BASE=${1:-http://localhost:8082}
case $BASE in http://localhost:* | http://127.0.0.1:*) ;; *) echo "local APIs only" >&2; exit 1 ;; esac

if [ "${2:-}" = pantry ]; then
    token=$(curl -sf -X POST "$BASE/api/v1/auth/dev" -H 'Content-Type: application/json' \
        -d '{"subject":"dev-simulator"}' | python3 -c 'import json,sys; print(json.load(sys.stdin)["accessToken"])')
    auth=(-H "Authorization: Bearer $token" -H 'Content-Type: application/json')
    hid=$(curl -sf "$BASE/api/v1/households" "${auth[@]}" |
        python3 -c 'import json,sys
items=json.load(sys.stdin).get("items") or []
print(items[0]["household"]["id"] if items else "")')
    [ -n "$hid" ] || { echo "dev-simulator has no household yet: sign in once in the app" >&2; exit 1; }
    have=$(curl -sf "$BASE/api/v1/households/$hid/pantry" "${auth[@]}" |
        python3 -c 'import json,sys; print(len(json.load(sys.stdin).get("items") or []))')
    if [ "$have" -eq 0 ]; then
        # name|quantity|unit|storage|status
        while IFS='|' read -r name qty unit storage status; do
            body=$(python3 -c 'import json,sys
n,q,u,s,st=sys.argv[1:]
d={"name":n,"storage":s,"status":st}
if q: d["quantity"]=q
if u: d["unit"]=u
print(json.dumps(d))' "$name" "$qty" "$unit" "$storage" "$status")
            curl -sf -X POST "$BASE/api/v1/households/$hid/pantry" "${auth[@]}" -d "$body" >/dev/null
        done <<'ITEMS'
Jasmine Rice|2|lb|pantry|in_stock
Spaghetti|16|oz|pantry|in_stock
Black Beans|2|can|pantry|in_stock
Olive Oil|12|floz|pantry|in_stock
Soy Sauce|5|floz|pantry|low
Yellow Onions|3||pantry|in_stock
Garlic|8|clove|pantry|in_stock
Eggs|10||fridge|in_stock
Butter|1|lb|fridge|in_stock
Whole Milk|||fridge|low
Cheddar Cheese|8|oz|fridge|in_stock
Greek Yogurt|32|oz|fridge|in_stock
Ground Beef|1|lb|freezer|in_stock
Frozen Peas|16|oz|freezer|in_stock
ITEMS
    fi
    echo "$hid"
    exit 0
fi

token=$(curl -sf -X POST "$BASE/api/v1/auth/dev" -H 'Content-Type: application/json' \
    -d '{"subject":"qa-starter-library-2","email":"starter@example.test"}' |
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
        # Cards show a time, which the review path test looks for.
        draft=$(echo "$draft" | python3 -c 'import json,sys; d=json.load(sys.stdin); d["totalMinutes"]=d.get("totalMinutes") or 30; print(json.dumps(d))')
        curl -sf -X POST "$BASE/api/v1/households/$hid/recipes" "${auth[@]}" -d "$draft" >/dev/null
    done
fi
echo "$hid"
