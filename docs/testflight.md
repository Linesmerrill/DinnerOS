# TestFlight test information

Copy these into App Store Connect → TestFlight → **Test Information** before
submitting a build for external (public link) testing.

## Beta App Description

DinnerOS helps a household plan the week's dinners together. Pick meals from
your recipe library or let Autopilot suggest a week, get one combined grocery
list, and keep track of what's already in the pantry.

## What to Test

- Sign in, create a household, and look around from the welcome screen.
- Add a recipe: import from HelloFresh, browse Shared Recipes, or paste a link
  to any recipe online.
- Plan a few dinners for this week (or let Autopilot suggest a week) and check
  the grocery list makes sense.
- Cook one with the cooking screen: are the amounts and steps right?
- Invite someone to your household (Household → Invite Someone).
- Anything confusing, broken, or slow: take a screenshot and tap Share Beta
  Feedback. Household → Feedback & Bugs explains how.

## Feedback Email

linesmerrill@gmail.com

## Privacy Policy URL

https://api.tlps.dev/privacy

(Marketing URL, if asked: https://api.tlps.dev)

## Beta App Review: sign-in information

Sign-in required: **No demo account needed.**

> DinnerOS uses Sign in with Apple (Google sign-in is also offered). Any Apple
> ID can sign in; the first sign-in creates a new account. After signing in,
> tap **Create a Household**, enter any name, and tap Create. The household
> starts with a starter recipe library, so you can plan meals right away. No
> invitation is needed. To delete the account: Household tab → **Delete
> Account** (also on the Create a Household screen). The privacy policy is
> linked on the sign-in screen and in the Household tab.

Uncheck "Sign-in required" or, if App Store Connect insists on credentials,
leave them blank and paste the note above into **Review Notes**.

Before submitting, check that `STARTER_RECIPES_HOUSEHOLD_ID` is set in
production, or drop the starter library sentence: without it a reviewer's new
household starts with no recipes.
