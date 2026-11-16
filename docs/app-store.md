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
| User Content: Photos or Videos | Yes (recipe photos someone takes, picks from their library, or finds with photo search; location and camera details are removed first) | Yes | App Functionality |
| User Content: Other User Content | Yes (recipes, notes, ratings, grocery lists, prices entered) | Yes | App Functionality |
| Other Data | Yes (rough weather and evening busyness labels for Autopilot) | Yes | App Functionality, Product Personalization |

Not collected: precise location, contacts, order screenshots (read on the phone and never
sent), calendar events, health, financial info, browsing history, search history,
diagnostics, device ID, advertising data. Photo search words pass through our server to Pexels and
aren't kept, so they aren't search history.

## In-app purchases

Group "DinnerOS Plus" (22576115). Both products are in Prepare for Submission and must be added
to the app version before it's submitted:

- `com.linesmerrill.dinneros.plus.yearly`: $39.99 a year, level 1, 2-week free trial
- `com.linesmerrill.dinneros.plus.monthly`: $4.99 a month, level 2, 2-week free trial

Review screenshots: the purchase screen with each plan selected (`docs/app-store/plus-review-yearly.png`, `docs/app-store/plus-review-monthly.png`).

## DinnerOS Plus at release

`PLUS_ENABLED` is off, so only `PLUS_TEST_HOUSEHOLDS` see the purchase screen. A reviewer signs
in with their own Apple ID, so their new household won't see Plus unless it's on for everyone.

**Plan:** set `PLUS_ENABLED=true` on Heroku when the build is submitted for review (no app update
needed). From then on, Autopilot and the cost view need Plus in every household, so TestFlight
testers without Plus see the purchase screen. To keep current testers whole, share an invite code
with them first (each gets a free month), or flip on the day of submission and tell them. If
review is rejected for a Plus reason, set it back to off; nothing else changes.

## App Review notes

Paste into App Review Information, Notes (also `ios/fastlane/metadata/review_information/notes.txt`).
There is no demo account: a reviewer signs in with their own Apple ID.

> DinnerOS is a dinner planner for households. To start, tap Continue with Apple and create a household. It comes with a library of recipes, so there is something to plan with right away.
>
> DinnerOS Plus: go to the Household tab and tap DinnerOS Plus. The purchase screen shows both plans (Monthly and Yearly), the 14-day free trial, the renewal terms, Restore purchases, and links to the Privacy Policy and Terms of Use. You can also reach it from the Menu tab by tapping Plan with Autopilot. After buying, Household > DinnerOS Plus opens Your Plus, where Manage Subscription, Restore Purchases and Request a Refund use Apple's own sheets.
>
> Invite codes: every household gets a code under Household > Give a Free Month to share with friends. Codes are free and never sold, and there is no way to buy one in the app or anywhere else. When a new household joins with a code and finishes setup (saves 3 recipes and plans a dinner), both households get a free month of Plus. The sharer can earn a limited number of months. The code does nothing for a household that already exists, and it only adds time to our own Plus access. It never replaces or changes an App Store purchase.
>
> Grocery stores: sending a list to Walmart works without signing in. It opens Walmart's site or app with the items, and you check out there. Kroger asks you to sign in with your own Kroger account before it can add items to your cart, so that step needs a Kroger account. DinnerOS never takes payment for groceries.
>
> Location, calendar and Reminders access are optional and only asked for when you turn on the feature that uses them.
>
> Account deletion: Household tab > Account > Delete Account.

Copyright: © 2026 Merrill Lines. The App Review contact is in `ios/fastlane/metadata/review_information/` on the release Mac only, never in git.

To submit: put the issuer ID and key file in the environment, then run `bundle exec fastlane submit_review build:<number>` in `ios/` (add `dry_run:true` to upload without submitting). It uploads `ios/fastlane/metadata`, the screenshots in `store-screenshots/`, the contact, both Plus subscriptions with their review screenshots, and submits with manual release.

**Demo account and Plus:** beta households got Plus on the house (decision 682) with a one-time
grant (`cmd/compbeta`). It left out PLUS_TEST_HOUSEHOLDS and anything passed with `-exclude`, and
it never touches households made later, so a demo account or a new account App Review creates
always sees the paywall and both subscriptions. If a demo household is made before the grant runs,
pass its ID with `-exclude`.

## Screenshots

Taken in the simulator with sample data (status bar 9:41, full battery), at the exact sizes App
Store Connect asks for. Upload in this order.

6.9" iPhone, 1320 x 2868 (`store-screenshots/iphone/`, not in git: they show starter recipe photos):

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
