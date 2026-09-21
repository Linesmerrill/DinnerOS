import Foundation

/// The script that reads a household's own order history, inside the web view, in the member's
/// own signed-in session (`docs/meal-kit-import.md`).
///
/// It runs in an isolated content world, so the page's own JavaScript cannot see the token it is
/// given or tamper with what it returns, and it reads nothing from the DOM: it makes the same
/// account requests the site itself makes, and returns **only** a list of recipe ids, public page
/// URLs and delivery weeks, plus where the walk stopped.
///
/// Politeness is the same rule the server keeps: one request at a time, a pause between them, a
/// hard page cap, and a stop as soon as a page is empty or the walk stops moving backwards.
///
/// A page covers roughly four to five delivered weeks — measured against a real account, which
/// returned 740 recipes across 160 weeks in 40 pages and **stopped on the cap** with years of
/// history still behind it. So the walk is given segments to cover (`MealKitHarvestPlan`) and
/// reports where it got to, and a later harvest resumes there instead of starting over.
nonisolated enum MealKitHarvestScript {
    /// How many pages of history one harvest walks, across all its segments. A page is a
    /// request. Four years of weekly deliveries does not fit, which is the whole reason the
    /// server keeps a cursor.
    static let maxPages = 40
    /// The pause between requests. Slower than a person clicking, fast enough that a member
    /// watching a spinner does not give up.
    static let minIntervalMilliseconds = 500
    /// How many recipes one harvest may return, matching the server's own cap.
    static let maxRecipes = 1000

    /// The arguments for one run of `body`, for `callAsyncJavaScript`.
    ///
    /// Pure, so what the script is asked to walk is tested rather than read off a simulator. The
    /// token goes in here and comes back out of nothing: the script never returns it.
    static func arguments(
        session: MealKitWebSession, service: MealKitService, subscription: String = "",
        history: MealKitImportHistory? = nil
    ) -> [String: Any] {
        [
            "token": session.accessToken,
            "tokenType": session.tokenType,
            "subscription": subscription,
            "country": service.country,
            "locale": service.locale,
            "maxPages": maxPages,
            "minIntervalMs": minIntervalMilliseconds,
            "maxRecipes": maxRecipes,
            "segments": MealKitHarvestPlan.segments(for: history).map(\.arguments),
        ]
    }

    /// The body of an async JavaScript function, for `callAsyncJavaScript`. Its arguments are
    /// the ones `arguments(session:service:subscription:history:)` builds.
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

        // past-deliveries wants the *numeric* subscription id, which the plans endpoint
        // calls legacySubscriptionId. A plan's own `id` (and the hf_plan_id cookie) is the
        // customer plan's UUID, and that answers 403 — measured, not assumed.
        function subscriptionIDOf(plan) {
            if (!plan) { return ""; }
            for (const value of [plan.legacySubscriptionId, plan.subscriptionId]) {
                const text = typeof value === "number" ? String(value) : value;
                if (typeof text === "string" && /^\\d+$/.test(text)) { return text; }
            }
            return "";
        }

        async function findSubscription() {
            if (/^\\d+$/.test(String(subscription || ""))) { return String(subscription); }
            const plans = await getJSON(origin + "/gw/api/plans?includeCanceled=false");
            const list = Array.isArray(plans) ? plans
                : (plans && Array.isArray(plans.items)) ? plans.items
                : (plans && Array.isArray(plans.plans)) ? plans.plans
                : [];
            for (const plan of list) {
                const id = subscriptionIDOf(plan);
                if (id) { return id; }
            }
            const none = new Error("no subscription");
            none.code = "unreadable";
            throw none;
        }

        try {
            const plan = await findSubscription();
            const byID = new Map();
            let pages = 0;
            let weeksSeen = 0;
            let earliestSeen = null;
            let latestSeen = null;

            // Walks one segment backwards from `start` (empty means this week) and stops at
            // `floor` — the newest week already imported — or at the start of the history.
            // A segment with a floor can never report the start of the history: an empty page
            // up here only means there is nothing new.
            async function walk(start, floor) {
                let from = start || isoWeekOf(new Date());
                for (;;) {
                    if (pages >= maxPages) { return "cap"; }
                    const url = origin + "/gw/my-deliveries/past-deliveries"
                        + "?country=" + encodeURIComponent(country)
                        + "&from=" + encodeURIComponent(from)
                        + "&locale=" + encodeURIComponent(locale)
                        + "&rating-scale=5"
                        + "&subscription=" + encodeURIComponent(plan);
                    const page = await getJSON(url);
                    pages += 1;
                    if (!page || !Array.isArray(page.weeks)) {
                        const shape = new Error("shape");
                        shape.code = "unreadable";
                        throw shape;
                    }
                    if (page.weeks.length === 0) { return floor ? "caught_up" : "empty"; }

                    let pageEarliest = null;
                    for (const week of page.weeks) {
                        const label = (week && typeof week.week === "string" && /^\\d{4}-W\\d{2}$/.test(week.week))
                            ? week.week : "";
                        if (label) {
                            if (pageEarliest === null || label < pageEarliest) { pageEarliest = label; }
                            if (earliestSeen === null || label < earliestSeen) { earliestSeen = label; }
                            if (latestSeen === null || label > latestSeen) { latestSeen = label; }
                        }
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

                    // Everything from here down has been read already.
                    if (floor && pageEarliest !== null && pageEarliest <= floor) { return "caught_up"; }
                    // Step back to the week before the earliest one this page returned. An
                    // unreadable week, or a step that does not move backwards, stops the walk
                    // instead of asking for the same page forever.
                    if (pageEarliest === null) { return floor ? "caught_up" : "end"; }
                    const monday = mondayOf(pageEarliest);
                    if (monday === null) { return floor ? "caught_up" : "end"; }
                    monday.setUTCDate(monday.getUTCDate() - 7);
                    const next = isoWeekOf(monday);
                    if (!(next < from)) { return floor ? "caught_up" : "end"; }
                    from = next;
                    await sleep(minIntervalMs);
                }
            }

            const plannedSegments = Array.isArray(segments) && segments.length > 0
                ? segments : [{ from: "", floor: "" }];
            let stopped = "end";
            let firstStop = "";
            for (const segment of plannedSegments) {
                stopped = await walk(segment && segment.from ? segment.from : "",
                                     segment && segment.floor ? segment.floor : "");
                if (firstStop === "") { firstStop = stopped; }
                // No point walking deeper once the page budget is spent.
                if (stopped === "cap") { break; }
            }
            // A catch-up that ran out of pages never reached today's end of the history, so its
            // newest week is not a ceiling the server may move up to: saying nothing leaves the
            // stored one alone and the next pass walks the gap.
            if (plannedSegments[0] && plannedSegments[0].floor && firstStop === "cap") { latestSeen = null; }

            const recipes = [];
            for (const entry of byID.values()) {
                entry.weeks.sort();
                recipes.push(entry);
            }
            return JSON.stringify({
                recipes: recipes,
                pages: pages,
                weeks: weeksSeen,
                earliestWeek: earliestSeen || "",
                latestWeek: latestSeen || "",
                stopped: stopped,
            });
        } catch (e) {
            // Only our own code, never the page's text.
            return fail((e && e.code) || "unavailable", e && e.status);
        }
        """
}
