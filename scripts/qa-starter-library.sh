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
#
# With "tidy" it clears what earlier UI test runs left in that same household, so the screenshot
# tests photograph one steady household (build 122: three Chicken Bowls on the Menu, 41 meals in
# one shot and 42 in the next). Tests that add, plan or delete now use households of their own
# (ios/DinnerOSUITests/QAOwnHousehold.swift); this removes what older runs left: recipes named
# "QA ...", recipes typed by hand other than the screenshots' own ("Sip Fudge", or the names in
# QA_KEEP_RECIPES, separated by "|"), second copies of a recipe (same name), a recipe planned twice
# in one week, and "QA Companion" companions. The meal-kit recipes the household was seeded with
# stay. Only things over 30 minutes old go, so a test running now keeps its own. Re-running it
# removes nothing new.
#   scripts/qa-starter-library.sh http://localhost:8082 tidy
set -euo pipefail
BASE=${1:-http://localhost:8082}
case $BASE in http://localhost:* | http://127.0.0.1:*) ;; *) echo "local APIs only" >&2; exit 1 ;; esac

if [ "${2:-}" = tidy ]; then
    token=$(curl -sf -X POST "$BASE/api/v1/auth/dev" -H 'Content-Type: application/json' \
        -d '{"subject":"dev-simulator"}' | python3 -c 'import json,sys; print(json.load(sys.stdin)["accessToken"])')
    BASE=$BASE TOKEN=$token python3 - <<'TIDY'
import datetime, json, os, urllib.request

base, token = os.environ["BASE"] + "/api/v1", os.environ["TOKEN"]

def call(method, path):
    req = urllib.request.Request(base + path, method=method, headers={"Authorization": "Bearer " + token})
    with urllib.request.urlopen(req) as r:
        body = r.read()
    return json.loads(body) if body else {}

def age_minutes(object_id):
    made = datetime.datetime.fromtimestamp(int(object_id[:8], 16), datetime.timezone.utc)
    return (datetime.datetime.now(datetime.timezone.utc) - made).total_seconds() / 60

items = call("GET", "/households").get("items") or []
if not items:
    raise SystemExit("dev-simulator has no household yet: sign in once in the app")
household = items[0]["household"]
hid = household["id"]

# Weeks a run could have planned in: four back to four ahead, by the household's first day.
order = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"]
index = order.index(household.get("weekStartsOn") or "mon")
offset = index - 7 if index > 3 else index
today = datetime.date.today()
weeks = sorted({"%04d-W%02d" % (today + datetime.timedelta(days=d - offset)).isocalendar()[:2] for d in range(-28, 36)})

planned, removed_entries = set(), 0
for week in weeks:
    plan = call("GET", f"/households/{hid}/plans/{week}")
    seen = set()
    for e in sorted(plan.get("entries") or [], key=lambda e: e.get("addedAt") or ""):
        recipe = e.get("recipe") or {}
        rid, name = recipe.get("id", ""), recipe.get("name", "")
        old = age_minutes(e["id"]) > 30
        if old and (rid in seen or name.startswith("QA ")):
            call("DELETE", f"/households/{hid}/plans/{week}/entries/{e['id']}")
            removed_entries += 1
            continue
        seen.add(rid)
        planned.add(rid)

recipes, cursor = [], ""
while True:
    page = call("GET", f"/households/{hid}/recipes?limit=100" + (f"&cursor={cursor}" if cursor else ""))
    recipes += page.get("items") or []
    cursor = page.get("nextCursor") or ""
    if not cursor:
        break
keep = set(filter(None, os.environ.get("QA_KEEP_RECIPES", "Sip Fudge").split("|")))
removed_recipes, names = 0, {}
for r in sorted(recipes, key=lambda r: r["id"]):
    first = names.setdefault(r["name"], r["id"])
    if r["id"] in planned or age_minutes(r["id"]) <= 30:
        continue
    extra = first != r["id"] or r["name"].startswith("QA ")
    # Typed by hand, pasted or read from a page: left by a person or a test, not the seed.
    if not extra and r["name"] not in keep:
        extra = call("GET", f"/households/{hid}/recipes/{r['id']}").get("source") in ("manual", "user")
    if extra:
        call("DELETE", f"/households/{hid}/recipes/{r['id']}")
        removed_recipes += 1

removed_companions = 0
for c in call("GET", f"/households/{hid}/companions").get("items") or []:
    if c.get("name", "").startswith("QA Companion ") and age_minutes(c["id"]) > 30:
        call("DELETE", f"/households/{hid}/companions/{c['id']}")
        removed_companions += 1
print(f"tidied {hid}: {removed_recipes} recipes, {removed_entries} planned meals, {removed_companions} companions removed")
TIDY
    exit 0
fi

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
