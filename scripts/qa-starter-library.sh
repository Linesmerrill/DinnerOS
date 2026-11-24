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
# "QA ...", recipes typed by hand other than the screenshots' own ("Pesto Bow Tie Pasta", or the
# names in QA_KEEP_RECIPES, separated by "|"), second copies of a recipe (same name), a recipe
# planned twice in one week, and "QA Companion" companions. The meal-kit recipes the household was
# seeded with stay. Only things over 30 minutes old go, so a test running now keeps its own. It also
# makes the screenshots' typed-by-hand recipe, with its photo, when it's missing, and moves the old
# one's ("Sip Fudge", or QA_RETIRED_RECIPES) planned meals to it. Re-running it changes nothing.
#   scripts/qa-starter-library.sh http://localhost:8082 tidy
#
# With "menu" it makes sure that household's current week has what the sweep checks on the Menu
# (build 123): a dinner on the week's first day, marked made so its card asks "How was it?", and
# the household's Garlic Bread planned with it as an add-on. Re-running it changes nothing.
#   scripts/qa-starter-library.sh http://localhost:8082 menu
set -euo pipefail
BASE=${1:-http://localhost:8082}
case $BASE in http://localhost:* | http://127.0.0.1:*) ;; *) echo "local APIs only" >&2; exit 1 ;; esac

if [ "${2:-}" = menu ]; then
    token=$(curl -sf -X POST "$BASE/api/v1/auth/dev" -H 'Content-Type: application/json' \
        -d '{"subject":"dev-simulator"}' | python3 -c 'import json,sys; print(json.load(sys.stdin)["accessToken"])')
    BASE=$BASE TOKEN=$token python3 - <<'MENU'
import datetime, json, os, urllib.request, uuid

base, token = os.environ["BASE"] + "/api/v1", os.environ["TOKEN"]

def call(method, path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    headers = {"Authorization": "Bearer " + token, "Content-Type": "application/json"}
    req = urllib.request.Request(base + path, data=data, method=method, headers=headers)
    with urllib.request.urlopen(req) as r:
        raw = r.read()
    return json.loads(raw) if raw else {}

items = call("GET", "/households").get("items") or []
if not items:
    raise SystemExit("dev-simulator has no household yet: sign in once in the app")
household = items[0]["household"]
hid, first_day = household["id"], household.get("weekStartsOn") or "mon"
week = call("GET", f"/households/{hid}/menu")["week"]
recipes, cursor = [], ""
while True:
    page = call("GET", f"/households/{hid}/recipes?limit=100" + (f"&cursor={cursor}" if cursor else ""))
    recipes += page.get("items") or []
    cursor = page.get("nextCursor") or ""
    if not cursor:
        break
by_name = {r["name"]: r for r in recipes}
plan = call("GET", f"/households/{hid}/plans/{week}")
entries = plan.get("entries") or []

# The made dinner: one already on the first day, else the meal kit's Chicken Penne al Limone (or
# the first dinner with a photo) planned there.
main = next((e for e in entries if e.get("day") == first_day and not (e.get("recipe") or {}).get("isAddon")), None)
made = ""
if main is None:
    planned = {(e.get("recipe") or {}).get("id") for e in entries}
    pick = by_name.get("Chicken Penne al Limone")
    if pick is None or pick["id"] in planned:
        pick = next(r for r in sorted(recipes, key=lambda r: r["name"])
                    if not r.get("isAddon") and r.get("imageUrl") and r["id"] not in planned)
    main = call("POST", f"/households/{hid}/plans/{week}/entries",
                {"recipeId": pick["id"], "day": first_day, "servings": 2})["entry"]
    made = f"planned {pick['name']}; "
if not main.get("outcome"):
    call("POST", f"/households/{hid}/events", {"events": [{
        "clientEventId": str(uuid.uuid4()), "type": "recipe.cooked", "recipeId": main["recipe"]["id"], "week": week,
        "occurredAt": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "payload": {"entryId": main["id"], "date": main.get("date"), "servings": main.get("servings") or 2},
    }]})
    made += f"marked {main['recipe']['name']} made; "

# The add-on: Garlic Bread (a meal-kit add-on with a photo) with that dinner.
if not any((e.get("recipe") or {}).get("isAddon") for e in entries):
    bread = by_name.get("Garlic Bread") or next(r for r in recipes if r.get("isAddon") and r.get("imageUrl"))
    call("POST", f"/households/{hid}/plans/{week}/entries",
         {"recipeId": bread["id"], "day": main.get("day") or first_day, "servings": 2})
    made += f"planned {bread['name']} as an add-on; "
print(f"menu for {hid} {week}: " + (made.rstrip("; ") or "already set"))
MENU
    exit 0
fi

if [ "${2:-}" = tidy ]; then
    token=$(curl -sf -X POST "$BASE/api/v1/auth/dev" -H 'Content-Type: application/json' \
        -d '{"subject":"dev-simulator"}' | python3 -c 'import json,sys; print(json.load(sys.stdin)["accessToken"])')
    BASE=$BASE TOKEN=$token python3 - <<'TIDY'
import datetime, json, os, subprocess, urllib.request

base, token = os.environ["BASE"] + "/api/v1", os.environ["TOKEN"]

def call(method, path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    headers = {"Authorization": "Bearer " + token, "Content-Type": "application/json"}
    req = urllib.request.Request(base + path, data=data, method=method, headers=headers)
    with urllib.request.urlopen(req) as r:
        raw = r.read()
    return json.loads(raw) if raw else {}

def age_minutes(object_id):
    made = datetime.datetime.fromtimestamp(int(object_id[:8], 16), datetime.timezone.utc)
    return (datetime.datetime.now(datetime.timezone.utc) - made).total_seconds() / 60

items = call("GET", "/households").get("items") or []
if not items:
    raise SystemExit("dev-simulator has no household yet: sign in once in the app")
household = items[0]["household"]
hid = household["id"]

def all_recipes():
    out, cursor = [], ""
    while True:
        page = call("GET", f"/households/{hid}/recipes?limit=100" + (f"&cursor={cursor}" if cursor else ""))
        out += page.get("items") or []
        cursor = page.get("nextCursor") or ""
        if not cursor:
            return out

TYPED_DESCRIPTION = (
    "Our quick weeknight pasta, on the table in about 20 minutes. Store-bought pesto is fine here, and a "
    "spoon of the pasta water is what makes it silky instead of oily, so don't skip saving some before you "
    "drain. Use whatever greens are in the fridge: baby spinach, arugula or kale all work, and the heat of "
    "the pasta wilts them in a minute. Halve the tomatoes so they soak up the sauce. Toasting the pine nuts "
    "takes two minutes and makes a big difference, but watch them closely because they burn fast.\n\n"
    "It's good warm, and just as good cold the next day for lunch, so it's worth making the whole box even "
    "for two. To make it more filling, add grilled chicken, white beans or a handful of frozen peas in the "
    "last minute of boiling. For the kids, leave out the red pepper flakes and add extra parmesan at the table."
)

# The screenshots' typed-by-hand recipe (QA_MANUAL_NAME in the matrix): a real dinner with a real
# photo, made here when it's missing. It replaced "Sip Fudge", two lines and no photo (build 122
# review items 3 and 16), wherever that was planned.
TYPED = {
    "name": "Pesto Bow Tie Pasta",
    # A few lines about the dish, as a person typing it in might add: the page then scrolls on the
    # upright iPad too, so every config has its "typed by hand › scrolled" shot (Hark's build 123
    # review, item 3).
    "description": TYPED_DESCRIPTION,
    "imageUrl": "https://images.unsplash.com/photo-1473093295043-cdd812d0e601?w=1080",
    "servings": 2, "prepMinutes": 5, "totalMinutes": 20, "cuisines": ["italian"], "cuisinesChosen": True,
    "tags": [], "warnings": [], "fromUrl": False,
    "ingredients": [
        {"name": "bow tie pasta", "quantity": "8", "unit": "oz", "pantryStaple": False},
        {"name": "basil pesto", "quantity": "1/3", "unit": "cup", "pantryStaple": False},
        {"name": "grape tomatoes", "quantity": "1", "unit": "cup", "pantryStaple": False, "note": "halved"},
        {"name": "baby spinach", "quantity": "2", "unit": "cup", "pantryStaple": False},
        {"name": "parmesan cheese", "quantity": "1/4", "unit": "cup", "pantryStaple": False, "note": "grated"},
        {"name": "lemon", "quantity": "1/2", "unit": "count", "pantryStaple": False},
        {"name": "olive oil", "quantity": "1", "unit": "tbsp", "pantryStaple": True},
        {"name": "garlic", "quantity": "2", "unit": "clove", "pantryStaple": False, "note": "minced"},
        {"name": "pine nuts", "quantity": "2", "unit": "tbsp", "pantryStaple": False},
        {"name": "fresh basil", "quantity": "1/4", "unit": "cup", "pantryStaple": False, "note": "torn"},
        {"name": "red pepper flakes", "quantity": "1/4", "unit": "tsp", "pantryStaple": True},
        {"name": "salt", "quantity": "", "unit": "", "pantryStaple": True},
        {"name": "black pepper", "quantity": "", "unit": "", "pantryStaple": True},
    ],
    "steps": [
        "Boil the pasta in salted water until tender, about 11 minutes. Save a splash of the water, then drain.",
        "Meanwhile, toast the pine nuts in a dry pan until golden, 2 to 3 minutes. Move them to a plate. "
        "Warm the oil in the same pan, add the garlic and red pepper flakes, and cook until fragrant, 30 seconds.",
        "Toss the hot pasta with the pesto, the garlic oil and the saved water until glossy.",
        "Fold in the tomatoes, spinach and a squeeze of lemon. Season with salt and pepper.",
        "Top with the parmesan, pine nuts and basil.",
    ],
}
# The typed recipe as it was first made (7 ingredients, no description) fits on the upright iPad's
# screen, page and cooking list alike, so that config had no scrolled shots (Hark's build 123 review,
# item 3). Bring an older copy up to date in place, keeping its planned meals: make the current one,
# copy its description, ingredients and steps onto the old one in the local database the QA APIs
# use (local Mongo only; the API can't edit a description), and delete the new one.
for old in [r for r in all_recipes() if r["name"] == TYPED["name"]]:
    full = call("GET", f"/households/{hid}/recipes/{old['id']}")
    full = full.get("recipe") or full
    if len(full.get("ingredients") or []) >= len(TYPED["ingredients"]) and full.get("description") == TYPED_DESCRIPTION:
        continue
    fresh = call("POST", f"/households/{hid}/recipes", TYPED)["id"]
    db = os.environ.get("QA_MONGO_DB", "dinneros_multistore")
    script = (
        "const d = db.getSiblingDB(%s).recipes;"
        "const f = d.findOne({_id: ObjectId(%s)});"
        "d.updateOne({_id: ObjectId(%s)}, {$set: {description: f.description, ingredients: f.ingredients,"
        " steps: f.steps, updatedAt: new Date()}})"
        % (json.dumps(db), json.dumps(fresh), json.dumps(old["id"])))
    subprocess.run(["mongosh", "--quiet", "mongodb://localhost:27017", "--eval", script], check=True,
                   stdout=subprocess.DEVNULL)
    call("DELETE", f"/households/{hid}/recipes/{fresh}")
retired = set(filter(None, os.environ.get("QA_RETIRED_RECIPES", "Sip Fudge").split("|")))
keep = set(filter(None, os.environ.get("QA_KEEP_RECIPES", TYPED["name"]).split("|")))
recipes = all_recipes()
typed_id = next((r["id"] for r in recipes if r["name"] == TYPED["name"]), "")
made_typed = not typed_id
if made_typed:
    typed_id = call("POST", f"/households/{hid}/recipes", TYPED)["id"]
retired_ids = {r["id"] for r in recipes if r["name"] in retired}

# Weeks a run could have planned in: four back to four ahead, by the household's first day.
order = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"]
index = order.index(household.get("weekStartsOn") or "mon")
offset = index - 7 if index > 3 else index
today = datetime.date.today()
weeks = sorted({"%04d-W%02d" % (today + datetime.timedelta(days=d - offset)).isocalendar()[:2] for d in range(-28, 36)})

planned, removed_entries, swapped = set(), 0, 0
for week in weeks:
    plan = call("GET", f"/households/{hid}/plans/{week}")
    seen = set()
    for e in sorted(plan.get("entries") or [], key=lambda e: e.get("addedAt") or ""):
        recipe = e.get("recipe") or {}
        rid, name = recipe.get("id", ""), recipe.get("name", "")
        if rid in retired_ids:
            # The retired screenshot recipe's place goes to the new one, same day and servings.
            call("DELETE", f"/households/{hid}/plans/{week}/entries/{e['id']}")
            if typed_id not in seen:
                call("POST", f"/households/{hid}/plans/{week}/entries",
                     {"recipeId": typed_id, "day": e.get("day"), "servings": e.get("servings") or 2})
                seen.add(typed_id)
                planned.add(typed_id)
                swapped += 1
            continue
        old = age_minutes(e["id"]) > 30
        if old and (rid in seen or name.startswith("QA ")):
            call("DELETE", f"/households/{hid}/plans/{week}/entries/{e['id']}")
            removed_entries += 1
            continue
        seen.add(rid)
        planned.add(rid)

removed_recipes, names = 0, {}
for r in sorted(all_recipes(), key=lambda r: r["id"]):
    first = names.setdefault(r["name"], r["id"])
    if r["name"] in keep and first == r["id"]:
        continue
    if r["id"] in retired_ids:
        call("DELETE", f"/households/{hid}/recipes/{r['id']}")
        removed_recipes += 1
        continue
    if r["id"] in planned or age_minutes(r["id"]) <= 30:
        continue
    extra = first != r["id"] or r["name"].startswith("QA ")
    # Typed by hand, pasted or read from a page: left by a person or a test, not the seed.
    if not extra:
        extra = call("GET", f"/households/{hid}/recipes/{r['id']}").get("source") in ("manual", "user")
    if extra:
        call("DELETE", f"/households/{hid}/recipes/{r['id']}")
        removed_recipes += 1

removed_companions = 0
for c in call("GET", f"/households/{hid}/companions").get("items") or []:
    if c.get("name", "").startswith("QA Companion ") and age_minutes(c["id"]) > 30:
        call("DELETE", f"/households/{hid}/companions/{c['id']}")
        removed_companions += 1
made = f"; made {TYPED['name']}" if made_typed else ""
print(f"tidied {hid}: {removed_recipes} recipes, {removed_entries} planned meals, {removed_companions} companions "
      f"removed, {swapped} planned meals moved to {TYPED['name']}{made}")
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
