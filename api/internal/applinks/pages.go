package applinks

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"strings"
)

// Paths of the static pages served by Handler.
const (
	HomePath    = "/"
	PrivacyPath = "/privacy"
)

// ContactEmail is where people reach the developer about privacy, account
// deletion, and TestFlight feedback. It is published on the pages.
const ContactEmail = "linesmerrill@gmail.com"

// PrivacyEffectiveDate is shown on the privacy policy. Change it whenever the
// policy's substance changes.
const PrivacyEffectiveDate = "September 16, 2026"

// staticPage is a rendered page with no script.
type staticPage struct {
	body []byte
	csp  string
}

func renderStaticPage(tmpl *template.Template, appName string) staticPage {
	var buf bytes.Buffer
	err := tmpl.Execute(&buf, map[string]any{
		"AppName":       appName,
		"ContactEmail":  ContactEmail,
		"EffectiveDate": PrivacyEffectiveDate,
		"PrivacyPath":   PrivacyPath,
		"Style":         template.CSS(pageStyle), //nolint:gosec // constant
	})
	if err != nil {
		panic(fmt.Sprintf("applinks: render %s: %v", tmpl.Name(), err))
	}
	return staticPage{
		body: buf.Bytes(),
		csp: strings.Join([]string{
			"default-src 'none'",
			"style-src '" + sha256Source(pageStyle) + "'",
			"base-uri 'none'",
			"form-action 'none'",
			"frame-ancestors 'none'",
		}, "; "),
	}
}

func (p staticPage) serve(w http.ResponseWriter, _ *http.Request) {
	header := w.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Content-Security-Policy", p.csp)
	header.Set("Cache-Control", "public, max-age=3600")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(p.body)
}

// pageStyle is the invitation page's look, without its button and notice.
const pageStyle = `
*{box-sizing:border-box}
body{margin:0;padding:24px 16px;background:#f6f2ee;color:#231f1b;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Helvetica,Arial,sans-serif;line-height:1.5;-webkit-text-size-adjust:100%}
main{max-width:640px;margin:0 auto;padding:32px 24px;background:#fff;border-radius:14px}
a{color:#c4561f}
.brand{margin:0 0 8px;font-size:14px;font-weight:600;color:#c4561f}
h1{margin:0 0 12px;font-size:24px;line-height:1.25}
h2{margin:28px 0 8px;font-size:17px}
p,ul{margin:0 0 12px;font-size:16px}
ul{padding-left:22px}
li{margin-bottom:6px}
.lead{margin-bottom:24px}
.note{color:#5f5750;font-size:15px}
.fine{margin:24px 0 0;color:#8a8179;font-size:13px}
`

var homePage = template.Must(template.New("home").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.AppName}}</title>
<style>{{.Style}}</style>
</head>
<body>
<main>
<p class="brand">{{.AppName}}</p>
<h1>Plan the week's dinners together</h1>
<p class="lead">{{.AppName}} is an iPhone app for households to plan dinners, build one grocery list, keep track of the pantry, and cook with less guesswork.</p>
<p class="note">{{.AppName}} is in testing on TestFlight with friends and family.</p>
<p><a href="{{.PrivacyPath}}">Privacy policy</a></p>
<p class="fine">Questions? Email <a href="mailto:{{.ContactEmail}}">{{.ContactEmail}}</a>.</p>
</main>
</body>
</html>
`))

var privacyPage = template.Must(template.New("privacy").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Privacy policy · {{.AppName}}</title>
<style>{{.Style}}</style>
</head>
<body>
<main>
<p class="brand"><a href="/">{{.AppName}}</a></p>
<h1>Privacy policy</h1>
<p class="note">Effective {{.EffectiveDate}}</p>
<p class="lead">{{.AppName}} is a small app built by one developer for planning household dinners. It collects only what it needs to work, and never sells your data or shows ads.</p>

<h2>What we collect</h2>
<ul>
<li><strong>Your account.</strong> When you sign in with Apple or Google we receive an account identifier from them, and your name and email address if you share them. With Sign in with Apple you can hide your email.</li>
<li><strong>Household data.</strong> Household names, members and their roles, time zone, servings, and invitations you send (including the invitee's email address).</li>
<li><strong>What you plan and keep.</strong> Recipes (including order history files you choose to import), weekly plans, grocery lists and skipped ingredients, pantry items and purchases, Autopilot preferences, store settings and saved products, and the grocery prices, order totals, and meal kit spend you enter.</li>
<li><strong>Ratings and activity in the app.</strong> Recipe ratings, and events such as viewing, planning, cooking, or skipping a recipe, which power suggestions for your household.</li>
<li><strong>Device push tokens,</strong> if you turn on notifications, so reminders can reach your phone.</li>
<li><strong>Server logs</strong> (request time, path, status, and IP address) kept briefly for security and debugging.</li>
</ul>
<p>{{.AppName}} has no advertising, no analytics or tracking SDKs, and does not track you across other apps or websites.</p>

<h2>Calendar and weather, processed on your iPhone</h2>
<p>If you allow it, Autopilot reads your calendars and the forecast for your approximate location on your iPhone when it plans a week, to suggest quicker dinners on busy evenings and cozier ones on cold or rainy days. Both are optional, asked for the first time you plan with Autopilot, and can be turned off in the app's Autopilot settings or in iOS Settings.</p>
<ul>
<li><strong>Calendar-derived busyness.</strong> For each day being planned, the app works out on your phone how busy the evening is (free, some, or busy) and about how many minutes are free. Only those values are sent. Event titles, times, attendees, locations, and notes never leave your phone.</li>
<li><strong>Approximate weather bands.</strong> The app uses reduced-accuracy location to get a forecast from Apple Weather on your phone and sends only a temperature band (cold, mild, or hot) and precipitation (none, rain, or snow) for each day. Your location and coordinates are never sent to us.</li>
</ul>
<p>These values are stored with that week's suggestions so swapping a meal uses the same context, and are deleted with them.</p>

<h2>Order screenshots, processed on your iPhone</h2>
<p>If you import prices from screenshots of a grocery order, the app reads the text in the images on your iPhone (and, where Apple Intelligence is available, organizes it on your iPhone too). The images and their text never leave your phone and aren't saved. Only the item prices and order total you review and confirm are sent, and they're stored with your household's grocery orders, pantry purchases, and saved products to show what your groceries cost each week. Prices and meal kit amounts you type in are stored the same way.</p>

<h2>How it's used</h2>
<p>Only to run the app for you and your household: signing you in, sharing plans with the members you invite, building grocery lists, suggesting meals, and sending the reminders you ask for. Members of your household can see its shared data and your name.</p>

<h2>Where it's stored and who processes it</h2>
<ul>
<li><strong>MongoDB Atlas</strong> stores the database, and <strong>Heroku</strong> runs the server.</li>
<li><strong>Resend</strong> sends invitation emails to the addresses you enter.</li>
<li><strong>Apple</strong> delivers push notifications, and Apple or Google verify your sign-in. If you allow weather, Apple Weather provides the forecast to your phone under Apple's privacy policy.</li>
<li>When you shop at a store such as Walmart, the app opens that store's website or app with your list; that store's own privacy policy applies there. Store links may carry an affiliate tag.</li>
</ul>
<p>We don't sell, rent, or share your data with anyone else, except if required by law.</p>

<h2>Deleting your data</h2>
<p>In the app, go to <strong>Household → Delete Account</strong>. This deletes your account, sign-ins, sessions, device tokens, and your ratings right away. Households where you were the only member are deleted with everything in them. Households you share stay with the other members (if you were the only admin, the longest-standing member becomes admin), and activity you recorded there no longer names you.</p>
<p>You can also email <a href="mailto:{{.ContactEmail}}">{{.ContactEmail}}</a> to ask for a copy or deletion of your data. Deleted data can remain in database backups until those backups expire.</p>

<h2>Children</h2>
<p>{{.AppName}} is not directed at children under 13, and we don't knowingly collect their data.</p>

<h2>Changes and contact</h2>
<p>If this policy changes, the new version will be posted here with a new effective date. Questions: <a href="mailto:{{.ContactEmail}}">{{.ContactEmail}}</a>.</p>
</main>
</body>
</html>
`))
