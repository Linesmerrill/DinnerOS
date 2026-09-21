import Foundation

/// The script that reads a household's own order history, inside the web view, in the member's
/// own signed-in session (`docs/meal-kit-import.md`).
///
/// It runs in an isolated content world, so the page's own JavaScript cannot see the token it is
/// given or tamper with what it returns, and it reads nothing from the DOM: it makes the same
/// account requests the site itself makes, and returns **only** a list of recipe ids, public page
/// URLs and delivery weeks. No page text, no cookie, and no token ever comes back out of it.
///
/// Politeness is the same rule the server keeps: one request at a time, a pause between them, a
/// hard page cap, and a stop as soon as a page is empty or the walk stops moving backwards.
nonisolated enum MealKitHarvestScript {
    /// How many pages of history one harvest walks. A page is a request.
    static let maxPages = 40
    /// The pause between requests. Slower than a person clicking, fast enough that a member
    /// watching a spinner does not give up.
    static let minIntervalMilliseconds = 500
    /// How many recipes one harvest may return, matching the server's own cap.
    static let maxRecipes = 1000

    /// The body of an async JavaScript function, for `callAsyncJavaScript`. Its arguments are
    /// `token`, `tokenType`, `subscription`, `country`, `locale`, `maxPages`, `minIntervalMs`
    /// and `maxRecipes`.
    static let body = """
        const auth = (tokenType || "Bearer") + " " + token;
        const origin = window.location.origin;

        const fail = (code, status) => JSON.stringify({ error: { code: code, status: status || 0 } });
        const sleep = (ms) => new Promise((done) => setTimeout(done, ms));

        async function getJSON(url) {
            // credentials are omitted on purpose: these endpoints answer to the
            // Authorization header, and we never want to widen what this sends.
            const response = await fetch(url, {
                method: "GET",
                credentials: "omit",
                headers: { "Authorization": auth, "Accept": "application/json" },
            });
            if (response.status === 401 || response.status === 403) {
                const denied = new Error("denied");
                denied.code = "forbidden";
                throw denied;
            }
            if (!response.ok) {
                const bad = new Error("http");
                bad.code = "unavailable";
                bad.status = response.status;
                throw bad;
            }
            try {
                return await response.json();
            } catch (e) {
                const shape = new Error("shape");
                shape.code = "unreadable";
                throw shape;
            }
        }

        function isoWeekOf(date) {
            const t = new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate()));
            t.setUTCDate(t.getUTCDate() - ((t.getUTCDay() + 6) % 7) + 3); // the Thursday of this week
            const year = t.getUTCFullYear();
            const jan4 = new Date(Date.UTC(year, 0, 4));
            const firstMonday = new Date(jan4);
            firstMonday.setUTCDate(jan4.getUTCDate() - ((jan4.getUTCDay() + 6) % 7));
            const week = Math.round((t - firstMonday) / 604800000) + 1;
            return year + "-W" + String(week).padStart(2, "0");
        }

        function mondayOf(week) {
            const match = /^(\\d{4})-W(\\d{2})$/.exec(week);
            if (!match) { return null; }
            const year = Number(match[1]);
            const jan4 = new Date(Date.UTC(year, 0, 4));
            const monday = new Date(jan4);
            monday.setUTCDate(jan4.getUTCDate() - ((jan4.getUTCDay() + 6) % 7) + (Number(match[2]) - 1) * 7);
            return monday;
        }

        async function findSubscription() {
            if (subscription) { return subscription; }
            const plans = await getJSON(origin + "/gw/api/plans?includeCanceled=false");
            const list = Array.isArray(plans) ? plans
                : (plans && Array.isArray(plans.items)) ? plans.items
                : (plans && Array.isArray(plans.plans)) ? plans.plans
                : [];
            for (const plan of list) {
                const id = plan && (plan.subscriptionId || plan.id);
                if (typeof id === "string" && id.length > 0) { return id; }
            }
            const none = new Error("no subscription");
            none.code = "unreadable";
            throw none;
        }

        try {
            const plan = await findSubscription();
            const byID = new Map();
            let from = isoWeekOf(new Date());
            let pages = 0;
            let weeksSeen = 0;

            while (pages < maxPages) {
                const url = origin + "/gw/my-deliveries/past-deliveries"
                    + "?country=" + encodeURIComponent(country)
                    + "&from=" + encodeURIComponent(from)
                    + "&locale=" + encodeURIComponent(locale)
                    + "&rating-scale=5"
                    + "&subscription=" + encodeURIComponent(plan);
                const page = await getJSON(url);
                pages += 1;
                if (!page || !Array.isArray(page.weeks)) {
                    return fail("unreadable");
                }
                if (page.weeks.length === 0) { break; }

                let earliest = null;
                for (const week of page.weeks) {
                    const label = (week && typeof week.week === "string" && /^\\d{4}-W\\d{2}$/.test(week.week))
                        ? week.week : "";
                    if (label && (earliest === null || label < earliest)) { earliest = label; }
                    weeksSeen += 1;
                    const groups = [[week.meals, false], [week.addons, true]];
                    for (const [items, isAddon] of groups) {
                        if (!Array.isArray(items)) { continue; }
                        for (const item of items) {
                            if (!item || typeof item.id !== "string") { continue; }
                            let entry = byID.get(item.id);
                            if (!entry) {
                                if (byID.size >= maxRecipes) { continue; }
                                entry = {
                                    sourceRecipeId: item.id,
                                    name: typeof item.name === "string" ? item.name : "",
                                    url: typeof item.websiteURL === "string" ? item.websiteURL : "",
                                    weeks: [],
                                    isAddon: isAddon,
                                };
                                byID.set(item.id, entry);
                            }
                            if (label && entry.weeks.indexOf(label) === -1) { entry.weeks.push(label); }
                        }
                    }
                }

                // Step back to the week before the earliest one this page returned. An
                // unreadable week, or a step that does not move backwards, stops the walk
                // instead of asking for the same page forever.
                if (earliest === null) { break; }
                const monday = mondayOf(earliest);
                if (monday === null) { break; }
                monday.setUTCDate(monday.getUTCDate() - 7);
                const next = isoWeekOf(monday);
                if (!(next < from)) { break; }
                from = next;
                await sleep(minIntervalMs);
            }

            const recipes = [];
            for (const entry of byID.values()) {
                entry.weeks.sort();
                recipes.push(entry);
            }
            return JSON.stringify({ recipes: recipes, pages: pages, weeks: weeksSeen });
        } catch (e) {
            // Only our own code, never the page's text.
            return fail((e && e.code) || "unavailable", e && e.status);
        }
        """
}
