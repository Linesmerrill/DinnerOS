#!/usr/bin/env python3
"""Builds docs/recipe-images/manifest.json from a starter recipe export.

    python3 scripts/recipe-image-manifest.py <export.json>

The export (id, name, ingredients, isAddon) is made from production and kept out of git.
Each entry's plating line is written from the title and its main ingredients, never from the
source's own text. Recipes in the App Store screenshots come first, flagged screenshot: true.
"""

import json
import re
import sys

SCREENSHOT = [
    "Cuban-Spiced Pork with Pineapple Salsa",
    "Chicken & Greek Salad Pita Pockets",
    "Beef Flautas Supreme",
    "Ginger Chicken & Rice Bowls",
    "Garlic Bread",
]

STAPLES = {"salt", "pepper", "black pepper", "olive oil", "vegetable oil", "cooking oil", "butter", "water",
           "sugar", "kosher salt", "flour", "all-purpose flour"}

COLORS = [
    (r"tomato|salsa|red pepper|bell pepper|chorizo|paprika|chili|sriracha|marinara|beet", "red"),
    (r"spinach|kale|zucchini|green bean|broccoli|cilantro|parsley|scallion|basil|pesto|arugula|peas|avocado|lime|cucumber|lettuce|asparagus", "green"),
    (r"pineapple|corn|lemon|cheddar|egg|mango|curry|turmeric", "yellow"),
    (r"carrot|sweet potato|orange|squash|pumpkin", "orange"),
    (r"rice|cauliflower|feta|mozzarella|cream|yogurt|sour cream|parmesan|potato|pasta|noodle|tortilla|pita", "cream"),
    (r"black bean|mushroom|balsamic|soy|teriyaki", "dark brown"),
    (r"red onion|cabbage|radish|eggplant", "purple"),
]

def vessel(name):
    n = name.lower()
    if re.search(r"bowl|soup|stew|chili|curry|ramen|noodle|risotto|chowder|bisque|pho|udon", n):
        return "in a wide bowl"
    if re.search(r"taco|flauta|quesadilla|pita|burger|sandwich|wrap|flatbread|pizza|melt|slider|crostini|bread", n):
        return "on a board or flat plate"
    return "on a plate"

def main_ingredients(ings, limit=4):
    out = []
    for name in ings:
        base = re.sub(r"\(.*?\)", "", name).strip()
        if base.lower() in STAPLES or len(out) >= limit:
            continue
        out.append(base.lower())
    return out

def colors(ings):
    text = " ".join(ings).lower()
    found = [c for pattern, c in COLORS if re.search(pattern, text)]
    return list(dict.fromkeys(found))[:3]

def plating(r):
    mains = main_ingredients(r["ingredients"])
    cols = colors(r["ingredients"])
    line = f"{r['name']}: {', '.join(mains) if mains else 'the dish'} served {vessel(r['name'])}"
    if cols:
        line += f", {', '.join(cols)} colors"
    return line + ", overhead, natural light, no text or logos."

def clean_title(name):
    return re.sub(r"\s*\bHello\s*Fresh\b\s*", " ", name, flags=re.I).strip()

def main():
    recipes = json.load(open(sys.argv[1]))
    order = {n: i for i, n in enumerate(SCREENSHOT)}
    recipes.sort(key=lambda r: (order.get(r["name"], len(SCREENSHOT)), r["name"]))
    entries = [{
        "id": r["id"],
        "title": clean_title(r["name"]),
        "plating": plating(r),
        "file": f"{r['id']}.jpg",
        "screenshot": r["name"] in order,
        "addon": r.get("isAddon", False),
    } for r in recipes]
    missing = [n for n in SCREENSHOT if n not in {r["name"] for r in recipes}]
    if missing:
        print("not in the export:", missing, file=sys.stderr)
    manifest = {
        "about": "One original AI food image per starter recipe, replacing the imported photos (decision 684). "
                 "Images go in docs/recipe-images/ as <id>.jpg, 1024x1024 JPEG, sRGB, about 200 KB, square, "
                 "dish centered with room to crop to 4:3 and 16:9. Screenshot recipes first.",
        "count": len(entries),
        "recipes": entries,
    }
    json.dump(manifest, open("docs/recipe-images/manifest.json", "w"), indent=2, ensure_ascii=False)
    open("docs/recipe-images/manifest.json", "a").write("\n")
    print(len(entries), "recipes,", sum(e["screenshot"] for e in entries), "screenshot")

if __name__ == "__main__":
    main()
