# DinnerOS website

Plain HTML and CSS. No build step, no scripts, no web fonts, no tracking.

Cloudflare Pages settings:

- Framework preset: None
- Build command: (leave empty)
- Build output directory: `website`
- Root directory: repo root

`_headers` sets security headers and caching. `404.html` is served for missing pages.
Each page repeats the same header and footer, so a nav change touches every page.

Preview locally: `python3 -m http.server 8090 --directory website`, then open http://localhost:8090.

Before launch: point `support@dinneros.app` somewhere real (Cloudflare Email Routing works),
and swap the "Coming soon to the App Store" placeholder for Apple's badge and the App Store link.
