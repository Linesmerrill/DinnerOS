# App Review account

The account App Review signs in with, household "Our Kitchen". Its email and password are only in
Heroku (`REVIEW_ACCOUNT_EMAIL` and `REVIEW_ACCOUNT_PASSWORD` on dinneros-api) and in App Store
Connect's Sign-In Information, never in the repo. Hark copies the password with
`heroku config:get REVIEW_ACCOUNT_PASSWORD -a dinneros-api`.

What the household has (decisions 752 and 759):

- Eight recipes DinnerOS wrote, each with a free Unsplash photo credited to its photographer.
  Nothing imported, no starter library, no HelloFresh content.
- No Plus on the house. A reviewer sees the normal purchase screen.
- This week planned: every sign-in plans the current week from the seed when nothing is planned
  yet, so the Menu, By Day and the grocery list are full whenever someone looks.

## Recipes and photos

| Recipe | Cuisine | Time | Photographer | Unsplash photo |
|---|---|---|---|---|
| Salmon with Asparagus and Rice | American | 30 min | [Solstice Hannan](https://unsplash.com/@darkersolstice) | https://unsplash.com/photos/salmon-over-rice-and-asparagus-q8NvIxPYvR8 |
| Beef and Broccoli | Chinese | 30 min | [sheri silver](https://unsplash.com/@sheri_silver) | https://unsplash.com/photos/a-bowl-of-beef-and-broccoli-with-white-rice-BA2UZ6w8JfY |
| Spaghetti Pomodoro | Italian | 30 min | [Paish Zaini](https://unsplash.com/@paishzaini) | https://unsplash.com/photos/a-white-plate-topped-with-spaghetti-and-tomatoes--9UJTnXpUXM |
| Shredded Beef Tacos | Mexican | 210 min | [Jeswin Thomas](https://unsplash.com/@jeswinthomas) | https://unsplash.com/photos/beef-tacos-with-onion-and-cilantro-z_PfaGzeN9E |
| Chicken Tikka Masala | Indian | 45 min | [Mohammad Fahim](https://unsplash.com/@dischef) | https://unsplash.com/photos/chicken-tikka-masala-with-naan-bread-on-a-wooden-board-ChM5be5nhis |
| Margherita Pizza | Italian | 35 min | [Joanie Simon](https://unsplash.com/@joaniesimon) | https://unsplash.com/photos/a-pizza-on-a-white-plate-on-a-table-T0q4_yyQ4Hg |
| Chicken Skewers with Garlic Sauce | Lebanese | 45 min | [Husien Bisky](https://unsplash.com/@husien_bisky1) | https://unsplash.com/photos/grilled-chicken-skewers-with-flatbread-and-pickles-RfpGZ2x6COA |
| Turkey and Bean Chili | American | 50 min | [Chad Montano](https://unsplash.com/@briewilly) | https://unsplash.com/photos/chili-bowl-with-cheese-and-cream-HNVi6m5KCr8 |

All eight are free under the [Unsplash License](https://unsplash.com/license), which allows
commercial use. The app hotlinks them from images.unsplash.com and shows "Photo by ... on
Unsplash", as the Unsplash API guidelines ask.

The week's plan, from the week's first day: Chicken Skewers with Garlic Sauce, Salmon with Asparagus and Rice, Chicken Tikka Masala, Spaghetti Pomodoro, Beef and Broccoli, Margherita Pizza, Shredded Beef Tacos. Turkey and Bean Chili is
left for the reviewer to add.

## Where the recipes come from

The recipes were typed into the app through Add a Recipe (Import From a Website, Paste Text),
with each one's servings, times and cuisine checked on the review step and its photo picked with
Choose Photo, Search the Web, the same way a person adds a recipe:

- `docs/review-account/recipes.txt`: the text that was pasted, one recipe per `===` block.
- `docs/review-account/photos.json`: each recipe's photo and photographer.
- `ios/DinnerOSUITests/ReviewAccountRecipesQATests.swift` does the typing, against a local API
  whose fake Unsplash (`api/cmd/fakeunsplash -photos docs/review-account/photos.json`) answers
  with these photos.

What the app saved was copied into `api/internal/reviewaccount/recipes.json`, the seed the
production API gives the review account. The production account is filled by the server from
that seed, so nobody types its password anywhere but App Store Connect.

The household made by build 116 had five earlier recipes without photos. Its next sign-in swaps
them for these eight; recipes a reviewer added stay.

## App Store screenshots

Taken from this account's content on a local API (same recipes, photos and plan) with a local
Plus trial so the Autopilot and Your Plus screens show. See docs/app-store.md, Screenshots.
