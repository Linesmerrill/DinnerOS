# App Store submission pack

Drafts for App Store Connect, kept in step with the app and dinneros.app. Nothing here is
submitted; Merrill decides when to send a build for review.

## Listing

| Field | Draft |
|---|---|
| Name (30) | DinnerOS |
| Subtitle (30) | Plan dinners. Shop once. |
| Category | Food & Drink (primary), Lifestyle (secondary) |
| Support URL | https://dinneros.app/support/ |
| Marketing URL | https://dinneros.app/ |
| Privacy Policy URL | https://dinneros.app/privacy/ |
| Terms of Use (EULA) | Use Apple's standard EULA in App Store Connect, and add the Terms link at the end of the description. https://dinneros.app/terms/ (also https://dinneros.app/eula) covers the subscription terms Apple requires. |
| Copyright | 2026 Merrill Lines (confirm the legal owner's name) |
| Price | Free, with DinnerOS Plus in-app subscriptions |

### Promotional text (170)

Plan the week's dinners with your household, get one grocery list, and send it to Walmart or Kroger in a tap. Try DinnerOS Plus free for 14 days.

### Description (4000)

DinnerOS helps your household decide what's for dinner, shop once for the week, and cook without the guesswork.

PLAN THE WEEK TOGETHER
Pick dinners for the week from your own recipes or the starter library. Everyone in your household sees the same plan and can swap a night when plans change.

ONE GROCERY LIST
DinnerOS turns the week's dinners into one list, combines what overlaps, and skips what's already in your pantry. Check things off in the store, or add the list to Reminders.

SEND IT TO A STORE
Send your list to Walmart or Kroger and finish checking out in their app. Sending a list is always free.

COOK STEP BY STEP
Each recipe opens to clear steps with built-in timers, so you know what to start first. Reminders tell you when to take something out to thaw.

MADE FOR HOUSEHOLDS
Invite the people you cook with. Each household has its own recipes, pantry, and plan.

DINNEROS PLUS
Plus adds Autopilot, which plans the week's dinners around how busy your evenings are and the weather, and shows what each week costs and how much you save against meal kits. One subscription covers everyone in your household.

- Monthly: $4.99 a month
- Yearly: $39.99 a year
- New subscribers get a 14-day free trial.

Plus renews automatically until you cancel. Payment is charged to your Apple account when you confirm a purchase, or when the free trial ends. It renews unless you cancel at least 24 hours before the end of the current period. Manage or cancel anytime in your Apple account settings.

Privacy Policy: https://dinneros.app/privacy/
Terms of Use: https://dinneros.app/terms/

### Keywords (100, comma separated, no spaces)

meal planner,dinner,grocery list,recipes,meal plan,family,household,shopping list,cooking,pantry

(96 characters. The name and subtitle words are already indexed, so they're left out.)

### What's New (first release)

The first App Store release of DinnerOS.

## Age rating answers

Every content question is **None** or **No**: no violence, mature themes, gambling, contests,
alcohol or drug references, medical information, or profanity. Unrestricted web access: **No**
(the only web view checks products on walmart.com). User-generated content shared outside the
household: **No** (recipes and plans are only seen by the household's own members). Result: **4+**.

## Export compliance

The app uses only Apple's standard HTTPS encryption. `ITSAppUsesNonExemptEncryption` is `false`
in Info.plist, so App Store Connect stops asking for each build.

## App Privacy (nutrition label)

Tracking: **No**. DinnerOS doesn't track people across other companies' apps or websites, and has
no ads or third-party analytics. `ios/DinnerOS/PrivacyInfo.xcprivacy` declares the same.

| Data type | Collected | Linked to the person | Used for |
|---|---|---|---|
| Contact Info: Name | Yes (from Sign in with Apple or Google) | Yes | App Functionality |
| Contact Info: Email Address | Yes | Yes | App Functionality |
| Identifiers: User ID | Yes (DinnerOS account and household IDs) | Yes | App Functionality |
| Location: Coarse Location | Yes (ZIP code, when someone looks for a store) | Yes | App Functionality |
| Purchases: Purchase History | Yes (Plus subscription events) | Yes | App Functionality |
| Usage Data: Product Interaction | Yes (planned, cooked, skipped, sent to a store) | Yes | App Functionality, Analytics, Product Personalization |
| User Content: Photos or Videos | Yes (recipe photos someone takes or picks from their library; location and camera details are removed first) | Yes | App Functionality |
| User Content: Other User Content | Yes (recipes, notes, ratings, grocery lists, prices entered) | Yes | App Functionality |
| Other Data | Yes (rough weather and evening busyness labels for Autopilot) | Yes | App Functionality, Product Personalization |

Not collected: precise location, contacts, order screenshots (read on the phone and never
sent), calendar events, health, financial info, browsing history, search history,
diagnostics, device ID, advertising data. Photo search words pass through our server to Unsplash and
aren't kept, so they aren't search history. A picked Unsplash photo is loaded from Unsplash.

## In-app purchases

Group "DinnerOS Plus" (22576115). Both products are in Prepare for Submission and must be added
to the app version before it's submitted:

- `com.linesmerrill.dinneros.plus.yearly`: $39.99 a year, level 1, 2-week free trial
- `com.linesmerrill.dinneros.plus.monthly`: $4.99 a month, level 2, 2-week free trial

Review screenshots: the purchase screen with each plan selected (`docs/app-store/plus-review-yearly.png`, `docs/app-store/plus-review-monthly.png`).

## DinnerOS Plus at release

`PLUS_ENABLED` is off, so only `PLUS_TEST_HOUSEHOLDS` see the purchase screen. Neither the demo
account's household nor one a reviewer makes with their own Apple ID is a test household, so
neither sees Plus unless it's on for everyone. Never add the demo household to
`PLUS_TEST_HOUSEHOLDS` or comp it: the reviewer must see the normal purchase screen.

**Plan:** set `PLUS_ENABLED=true` on Heroku when the build is submitted for review (no app update
needed). From then on, Autopilot and the cost view need Plus in every household, so TestFlight
testers without Plus see the purchase screen. To keep current testers whole, share an invite code
with them first (each gets a free month), or flip on the day of submission and tell them. If
review is rejected for a Plus reason, set it back to off; nothing else changes.

## App Review notes

Paste into App Review Information, Notes (also `ios/fastlane/metadata/review_information/notes.txt`).
The notes point App Review at a demo account (decision 752), and a reviewer can still use their
own Apple ID.

**Demo account.** One account signs in with an email and a password: Sign in with email, a small
link under Continue with Apple and Google, shows only while the API has `REVIEW_ACCOUNT_EMAIL`
and `REVIEW_ACCOUNT_PASSWORD` set on Heroku (`GET /api/v1/auth/options`). The repo is public, so
neither value is ever in git. To set it up for a submission:

1. Pick an email we control (it's only a sign-in name; nothing is sent to it) and a password of
   at least 16 characters, and set both on Heroku: `heroku config:set REVIEW_ACCOUNT_EMAIL=...
   REVIEW_ACCOUNT_PASSWORD=...`. The startup log says `reviewAccount=true`, never the values.
2. Put the same two in `ios/fastlane/metadata/review_information/demo_user.txt` and
   `demo_password.txt` on the release Mac (both are in `.gitignore`). `submit_review` sends them
   as the Sign-In Information and marks a demo account as required; without both files it sends
   none, as before.
3. Sign in once on a phone to check: the household "Our Kitchen" with eight recipes we wrote, each with a photo, and this week planned
   (`api/internal/reviewaccount/recipes.json`), no starter library, and the normal Plus purchase
   screen (no Plus on the house).

Only that one email works. A wrong email or password gets "That email and password didn't work."
Each address gets 5 tries, then one a minute, and 5 wrong tries in 15 minutes lock it out for 15
minutes. The household is seeded the first time the account signs in; after that it stays as the
reviewer left it. Deleting the account works as for anyone, and the next sign-in makes a fresh
seeded household. To turn it off after review, unset both variables.

The notes:

> DinnerOS plans a household's dinners for the week, makes one grocery list, and walks you through cooking.
>
> Demo account: on the sign-in screen, tap Sign in with email (under Continue with Apple), then enter the email and password in the Sign-In Information fields. It opens a household with eight recipes and this week's dinners already planned, so every screen has something in it. You can also sign in with your own Apple ID, then Create a Household and give it any name. A new household comes with a small library of recipes.
>
> Things to try:
> 1. Menu tab: tap a recipe to open it, then Start Cooking for the steps and timers.
> 2. Menu tab: tap the ... button for Add a Recipe (write one, or import one from a recipe link), Shared Recipes and Companions.
> 3. Menu tab: By Day shows the week night by night.
> 4. Shop tab: the week's grocery list. Pick Walmart to choose products and send the list.
> 5. Household tab: members, invites and your account.
>
> DinnerOS Plus is an auto-renewing subscription: Monthly ($4.99) or Yearly ($39.99), each with a 14-day free trial. Plus turns on Autopilot, which picks the week's dinners for you, and shows what each week costs. To see the purchase screen, tap Plan with Autopilot on the Menu tab, or go to Household > DinnerOS Plus. It shows both plans, the trial, the renewal terms, Restore purchases, and links to the Privacy Policy and Terms of Use. You can buy with a sandbox Apple Account. After buying, Household > DinnerOS Plus opens Your Plus, where Manage Subscription, Restore Purchases and Request a Refund use Apple's own screens.
>
> Free month (optional, not needed for review): Household > Give a Free Month shows a code to share with another household. When a new household enters it and finishes the setup steps the app lists, both households get a free month of Plus. The sharer can earn a limited number of months, and a code does nothing for a household that already exists. Codes are free and never sold, and there is no way to buy one. The free month itself is an Apple offer code: Redeem opens Apple's own redeem screen, which starts the Plus subscription with the first month free and says it renews unless cancelled. Apple's offer codes only work once the app is on the App Store, so a code can't be redeemed during review.
>
> Grocery stores (optional): sending a list to Walmart works without signing in. It opens Walmart's site or app with the items, and you check out there. Kroger and its other stores ask you to sign in with your own Kroger account before items go into a cart, so that step needs a real store account. DinnerOS never takes payment for groceries.
>
> Location, calendar, Reminders, camera and notifications are optional. The app only asks when you use the feature that needs them, and everything else works if you say no.
>
> Delete your account: Household > Account > Delete Account.

Copyright: © 2026 Merrill Lines. The App Review contact is in `ios/fastlane/metadata/review_information/` on the release Mac only, never in git.

To submit: put the issuer ID and key file in the environment, then run `bundle exec fastlane submit_review build:<number>` in `ios/` (add `dry_run:true` to upload without submitting). It uploads `ios/fastlane/metadata`, the screenshots in `store-screenshots/`, the contact, both Plus subscriptions with their review screenshots, and submits with manual release.

**Demo account and Plus:** beta households got Plus on the house (decision 682) with a one-time
grant (`cmd/compbeta`). It left out PLUS_TEST_HOUSEHOLDS and anything passed with `-exclude`, and
it never touches households made later, so a demo account or a new account App Review creates
always sees the paywall and both subscriptions. If a demo household is made before the grant runs,
pass its ID with `-exclude`.

## Screenshots

Taken in the simulator with sample data (status bar 9:41, full battery), at sizes App Store
Connect takes for the largest iPhone and iPad. Upload in this order.

They come from the App Review account's content (docs/review-account.md): our own eight recipes
with credited Unsplash photos, this week planned, Walmart chosen, Autopilot set up and a local
Plus trial. Nothing imported and no HelloFresh content. To retake them: start a local API on its
own database with `REVIEW_ACCOUNT_*` set and a fake Unsplash run with
`-photos docs/review-account/photos.json`, run `ReviewAccountRecipesQATests` to type the recipes
in, plan five dinners, set Autopilot up, choose Walmart and send a local RevenueCat trial event.
Then run `StoreScreenshotsQATests` with `TEST_RUNNER_QA_REVIEW_EMAIL`,
`TEST_RUNNER_QA_REVIEW_PASSWORD` and `TEST_RUNNER_QA_PLANNED_RECIPE="Chicken Tikka Masala"`
(`testPhoneScreenshots` on an iPhone 17 Pro Max, `testPadScreenshots` on an iPad Pro 13-inch)
after `xcrun simctl ui <id> appearance light` and `xcrun simctl status_bar <id> override --time
9:41 --batteryState charged --batteryLevel 100 --wifiBars 3 --cellularBars 4`. Each shot is a
named attachment in the result bundle; the iPad ones come out portrait and are turned to
landscape. `06-cook-steps.png` is the `06-cook-steps-timer` shot. Leave out the Members and
Account shots: they show the review account's name and email.

6.9" iPhone, 1320 x 2868 (`store-screenshots/iphone/`):

1. `01-menu.png` This week's dinners
2. `02-week.png` The week by day
3. `03-autopilot.png` Autopilot's picks
4. `04-shop.png` One list, sent to Walmart
5. `05-recipe.png` A recipe
6. `06-cook-steps.png` Cooking with a timer running
7. `07-timer.png` Setting a timer
8. `08-your-plus.png` Your Plus

13" iPad, 2752 x 2064 landscape (`store-screenshots/ipad/`):

1. `01-menu.png` This week's dinners
2. `02-recipe.png` A recipe
3. `03-cook.png` Ingredients and steps side by side

Plus review screenshots: `plus-review-yearly.png`, `plus-review-monthly.png`.
