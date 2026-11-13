# DinnerOS website

Plain HTML and CSS. No build step, no scripts, no web fonts, no tracking.

Cloudflare Pages settings:

- Framework preset: None
- Build command: (leave empty)
- Build output directory: `website`
- Root directory: repo root

`_headers` sets security headers and caching. CSS and JS are linked with `?v=<timestamp>`: bump it on every page when either file changes, or phones keep the old file. `404.html` is served for missing pages.
Each page repeats the same header and footer, so a nav change touches every page. On phones the
nav is a `<details>` Menu, so it works without scripts (the CSP blocks them).

The app pictures on the home page (the week, the list, the store send, timers, reminders) are
HTML and CSS copies of real app screens, using the app's own wording. No stock photos, so there's
nothing to credit. Keep them in step with the app when its wording changes.

Preview locally: `python3 -m http.server 8090 --directory website`, then open http://localhost:8090.

## Domain: dinneros.app

The site lives at the apex, `https://dinneros.app`. `www` redirects to it.

1. Pages project > Custom domains: add `dinneros.app`, then `www.dinneros.app`. The zone is on
   the same Cloudflare account, so Pages adds the DNS records itself.
2. dinneros.app zone > Rules > Redirect Rules: create a rule
   - When: Hostname equals `www.dinneros.app`
   - Then: Dynamic redirect to `concat("https://dinneros.app", http.request.uri.path)`,
     status 301, preserve query string.
3. SSL/TLS > Edge Certificates: turn on Always Use HTTPS.
4. Email > Email Routing: route `support@dinneros.app` to the inbox that answers support.

Canonical links, `sitemap.xml`, and `robots.txt` all use `https://dinneros.app`. Preview
deployments on `*.pages.dev` send `noindex` so search engines only list the real domain.

Before launch: swap the "Coming soon to the App Store" placeholder for Apple's badge and the
App Store link.
