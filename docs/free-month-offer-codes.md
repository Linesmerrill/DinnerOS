# Plan: the invite free month through Apple Offer Codes

Status: built for build 116 (decisions 755 to 758), after Merrill chose it on 2026-11-17. What's
below is the plan as written; where the build differs: one offer on the monthly subscription for
both households (new, existing and expired eligibility) instead of two, and the code pool is
`plus_offer_codes` with the months in `plus_free_months`. The App Store Connect settings and the
import steps are in docs/deployment.md, "Free month codes".

Before this, the free month was a date in our own database (`plus_free_time`, granted in
`api/internal/plus/qualify.go` `settle`), which the API treats as Plus. Apple and RevenueCat never
saw it. That unlocks paid features outside in-app purchase, which guideline 3.1.1 restricts.

## What changes

1. **Codes.** In App Store Connect, create a one-month free Offer Code offer on both Plus
   subscriptions (monthly and yearly), as one-time-use codes. Make two offers, because Apple sets
   eligibility per offer:
   - "Invite, new" for people who have never subscribed (the household that used the code).
   - "Invite, thank you" for current and lapsed subscribers (the sharer).
   Hark downloads each batch as a CSV.
2. **Code pool.** A staff route imports the CSV into a new `plus_offer_codes` collection
   (code, offer, batch, expiry, given to household, given at). Codes are secrets: never logged,
   never in the repo, deleted once they're used or expired.
3. **Granting.** `settle` stops writing `plus_free_time`. Instead it takes one unused code from the
   right offer for each household and records it. If the pool is empty, it records "owed" and an
   alert email goes to `REPORT_ALERT_EMAIL`, so nobody loses a month.
4. **In the app.** The free month card says "Your free month is ready" with a Redeem button. It
   opens Apple's redeem sheet with the code filled in
   (`AppStore.presentOfferCodeRedeemSheet`, or `apps.apple.com/redeem?ctx=offercodes&id=<app id>&code=<code>`).
   Apple shows the terms and starts the subscription. RevenueCat sees the redemption, and the
   API's Plus check follows RevenueCat as it already does for purchases.
5. **Existing free months.** Dates already granted keep working until they end. No new ones.

## What gets better and what gets worse

- Better: every month of Plus goes through Apple, so the 3.1.1 risk goes away. Refunds, Family
  Sharing and renewals behave like any purchase.
- Worse: redeeming starts a real subscription. The person needs an Apple account with a payment
  method, and after the free month it renews at the normal price unless they cancel. Today the
  free month just ends. The card has to say that plainly before they tap Redeem.
- Worse: someone has to keep the code pool stocked. Codes expire, and Apple limits how many
  can be made each quarter.

## Smaller step if Merrill wants one first

Keep the date for households that already earned it, and turn off new grants
(a new switch) while App Review looks at the app. Then ship the
Offer Codes version after approval.

## Work

About two days: the import route and collection, the `settle` change and tests, the redeem card
on iOS, the owed alert, copy, and a UI test that checks the redeem sheet opens. No RevenueCat
change is needed, because it already reads Offer Code redemptions.
