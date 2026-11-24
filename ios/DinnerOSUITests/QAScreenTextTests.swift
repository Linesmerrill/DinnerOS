import XCTest

/// The test-wording rule every screenshot is held to (QAScreenText in QAHelpers.swift), and the
/// realistic people UI tests sign in as (QAPeople). No app launch.
final class QAScreenTextTests: XCTestCase {
    func testTheWordsOnTheirOwnAreCaught() {
        for text in [
            "QA Sesame Pork Bowls", "QA Beef Tacos", "Skillet Test Tacos", "Tester", "Add QA Invite Soup",
            "Photo by QA Photographer on Unsplash", "Kitchen (QA)", "QA", "Rivera QA-Kitchen", "Test 2",
        ] {
            XCTAssertTrue(QAScreenText.showsTestWording(text), text)
        }
    }

    func testOtherWordsAndCasesPass() {
        for text in [
            "Sesame Pork Bowls", "Contest Winner Chili", "Testing your connection", "TestFlight", "Attest",
            "qa-new-12345", "Qa New", "AQAB", "Tests", "Testers", "Latest", "Protest", "QAT", "Rivera Kitchen", "",
        ] {
            XCTAssertFalse(QAScreenText.showsTestWording(text), text)
        }
    }

    /// Build 123 review, item 2: "Didn't make it?" at the right of the first card, the others
    /// shorter. Then the fix: one height, the row at the left edge, the stars level (Merrill,
    /// Nov 23: stars on every card).
    func testMealCardsLineUp() {
        typealias Card = MenuQAChecks.LinedUpCard
        let before = [
            Card(
                name: "Pasta", frame: CGRect(x: 20, y: 300, width: 380, height: 520),
                outcome: CGRect(x: 280, y: 700, width: 120, height: 20)),
            Card(name: "Garlic Bread", frame: CGRect(x: 412, y: 300, width: 380, height: 400), outcome: nil),
        ]
        let found = MenuQAChecks.lineUpProblems(before, screen: "Menu")
        XCTAssertEqual(found.count, 2, "\(found)")
        XCTAssertTrue(found.contains { $0.contains("400 points tall") })
        XCTAssertTrue(found.contains { $0.contains("points in from the card’s left edge") })

        let after = [
            Card(
                name: "Pasta", frame: CGRect(x: 20, y: 300, width: 380, height: 520),
                outcome: CGRect(x: 20, y: 700, width: 120, height: 20),
                stars: CGRect(x: 20, y: 600, width: 200, height: 50)),
            Card(
                name: "Garlic Bread", frame: CGRect(x: 412, y: 300, width: 380, height: 521),
                outcome: nil, stars: CGRect(x: 412, y: 600.5, width: 200, height: 50)),
        ]
        XCTAssertEqual(MenuQAChecks.lineUpProblems(after, screen: "Menu"), [])
        // Offered on both, but one sits lower.
        var lower = after
        lower[1].outcome = CGRect(x: 412, y: 740, width: 120, height: 20)
        XCTAssertEqual(MenuQAChecks.lineUpProblems(lower, screen: "Menu").count, 1)
        // Stars at different heights.
        var uneven = after
        uneven[1].stars = CGRect(x: 412, y: 640, width: 200, height: 50)
        XCTAssertEqual(MenuQAChecks.lineUpProblems(uneven, screen: "Menu").count, 1)
    }

    /// Build 123 review, item 7: the search result's add button 200 points in from the edge of an
    /// iPad in portrait; at the 20-point margin it passes.
    func testSearchAddButtonAtTheMargin() {
        let screen = CGRect(x: 0, y: 0, width: 834, height: 1194)
        XCTAssertEqual(
            MenuQAChecks.searchAddProblems(
                button: CGRect(x: 590, y: 360, width: 44, height: 44), screen: screen, name: "s"
            ).count,
            1)
        XCTAssertEqual(
            MenuQAChecks.searchAddProblems(
                button: CGRect(x: 770, y: 360, width: 44, height: 44), screen: screen, name: "s"),
            [])
    }

    func testOffendingListsEachTextOnce() {
        XCTAssertEqual(
            QAScreenText.offending(["Beef Tacos", "QA Beef Tacos", "QA Beef Tacos", "Test Kitchen"]),
            ["QA Beef Tacos", "Test Kitchen"])
        XCTAssertEqual(QAScreenText.offending(["Beef Tacos", "Contest"]), [])
    }

    /// Shared Recipes' rows fail on a test run's recipe (build 123 review: "Saffron Pilaf 90901"
    /// in Chef’s Picks).
    func testRunNumberedRecipeNamesAreCaught() {
        for name in ["Lemon Herb Pilaf 24421", "Mushroom Pilaf 90901", "Saffron Pilaf 123", "Pilaf  45678 "] {
            XCTAssertTrue(MenuQAChecks.endsInRunNumber(name), name)
        }
        for name in ["Saffron Pilaf", "Chili for 2", "Serves 4", "Soup for 12", "24421 Pilaf", "Route 66 Burger", ""] {
            XCTAssertFalse(MenuQAChecks.endsInRunNumber(name), name)
        }
        XCTAssertEqual(MenuQAChecks.cardName("Saffron Pilaf 90901, 25 min, Indian"), "Saffron Pilaf 90901")
        XCTAssertEqual(MenuQAChecks.cardName("Beef Tacos"), "Beef Tacos")
    }

    func testPeopleAreNamedLikePeople() {
        let subject = QAPeople.subject("Maya Rivera")
        XCTAssertTrue(subject.hasPrefix("maya-rivera-"), subject)
        XCTAssertEqual(QAPeople.displayName(forSubject: subject), "Maya Rivera")
        XCTAssertEqual(QAPeople.kitchen(forSubject: subject), "Rivera Kitchen")
        for name in QAPeople.names {
            XCTAssertFalse(QAScreenText.showsTestWording(name), name)
            XCTAssertEqual(QAPeople.displayName(forSubject: QAPeople.subject(name)), name)
        }
    }

    /// Build 121 review round 2, Hark's item 10: the count leaves out add-ons, and a meal past
    /// the edge shows a peek.
    func testMealCountAndPeek() {
        XCTAssertEqual(MenuQAChecks.mealCount(in: "3 meals planned · 1 add-on"), 3)
        XCTAssertEqual(MenuQAChecks.mealCount(in: "1 meal planned"), 1)
        XCTAssertNil(MenuQAChecks.mealCount(in: "Nothing planned"))
        XCTAssertEqual(MenuQAChecks.countProblems(label: "3 meals", promised: 3, mealCards: 3, screen: "Menu"), [])
        XCTAssertEqual(MenuQAChecks.countProblems(label: "3 meals", promised: 3, mealCards: 2, screen: "Menu").count, 1)
        let screen = CGRect(x: 0, y: 0, width: 1210, height: 834)
        let flush = [0.0, 403, 806, 1210].map { CGRect(x: $0, y: 300, width: 387, height: 400) }
        XCTAssertEqual(MenuQAChecks.peekProblems(cards: flush, screen: screen, name: "Menu").count, 1)
        let peek = [20.0, 387, 754, 1121].map { CGRect(x: $0, y: 300, width: 351, height: 400) }
        XCTAssertEqual(MenuQAChecks.peekProblems(cards: peek, screen: screen, name: "Menu"), [])
    }

    /// Hark's item 9: By Day starts at the top with every day of the week in order.
    func testByDayShowsTheWholeWeek() {
        typealias Header = (id: String, frame: CGRect)
        func header(_ id: String, _ y: CGFloat) -> Header { (id, CGRect(x: 20, y: y, width: 300, height: 20)) }
        let atTop = [header("byDay.unscheduled", 160), header("byDay.day.sun", 300)]
        XCTAssertEqual(MenuQAChecks.byDayTopProblems(atTop, stripBottom: 150, screen: "By Day"), [])
        let scrolled = [header("byDay.unscheduled", 120), header("byDay.day.mon", 260)]
        XCTAssertEqual(MenuQAChecks.byDayTopProblems(scrolled, stripBottom: 150, screen: "By Day").count, 1)
        XCTAssertEqual(MenuQAChecks.byDayGapProblems(firstCardTop: 240, stripBottom: 240, screen: "By Day").count, 1)
        XCTAssertEqual(MenuQAChecks.byDayGapProblems(firstCardTop: 280, stripBottom: 240, screen: "By Day"), [])
        let week = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"].map { "byDay.day." + $0 }
        XCTAssertEqual(MenuQAChecks.byDayWeekProblems(week, screen: "By Day"), [])
        XCTAssertEqual(MenuQAChecks.byDayWeekProblems(Array(week.dropFirst()), screen: "By Day").count, 1)
        let mondayFirst = Array(week[1...]) + [week[0]]
        XCTAssertEqual(MenuQAChecks.byDayWeekProblems(mondayFirst, screen: "By Day"), [])
        XCTAssertEqual(MenuQAChecks.byDayWeekProblems([week[1], week[0]] + week[2...], screen: "By Day").count, 1)
    }

    /// Hark's items 12, 13 and 11: suggestion widths, made meals in New to You, centered rows.
    func testSuggestedWidthsNewToYouAndCenteredRows() {
        XCTAssertEqual(MenuQAChecks.widthProblems(suggested: [350, 351], meals: [347], screen: "Menu"), [])
        XCTAssertEqual(MenuQAChecks.widthProblems(suggested: [575], meals: [360], screen: "Menu").count, 1)
        XCTAssertEqual(
            MenuQAChecks.madeInNewToYouProblems(
                made: ["Chicken Penne"], newToYou: ["Tacos", "Chicken Penne"], screen: "Menu"),
            ["Menu: “Chicken Penne” is marked made and still New to You"])
        XCTAssertEqual(
            MenuQAChecks.madeInNewToYouProblems(made: ["Chicken Penne"], newToYou: ["Tacos"], screen: "Menu"), [])
        let screen = CGRect(x: 0, y: 0, width: 1210, height: 834)
        let left = [CGRect(x: 20, y: 200, width: 690, height: 300)]
        XCTAssertEqual(MenuQAChecks.centeredProblems(spans: left, screen: screen, name: "Shared").count, 1)
        let centered = [
            CGRect(x: 260, y: 200, width: 690, height: 300), CGRect(x: 260, y: 600, width: 450, height: 300),
        ]
        XCTAssertEqual(MenuQAChecks.centeredProblems(spans: centered, screen: screen, name: "Shared"), [])
        let overflowing = [CGRect(x: 20, y: 200, width: 1500, height: 300)]
        XCTAssertEqual(MenuQAChecks.centeredProblems(spans: overflowing, screen: screen, name: "Shared"), [])
    }

    /// Hark's build 121 review, item 8: Account showed "hannah.kim395@example.test". An email
    /// reads like a person's (no digits, no .test) and stays on a domain kept for examples.
    func testEmailsLookLikePeoplesOnExampleDomains() {
        let pattern = #"^hannah(\.[a-z]\.kim|[a-z]kim)@example\.(com|net|org)$|^h[a-z]kim@example\.(com|net|org)$"#
        var seen = Set<String>()
        for _ in 0..<200 {
            let email = QAPeople.email(for: "Hannah Kim")
            XCTAssertNotNil(email.range(of: pattern, options: .regularExpression), email)
            XCTAssertFalse(email.contains { $0.isNumber }, email)
            seen.insert(email)
        }
        XCTAssertGreaterThan(seen.count, 60, "Emails repeat too often for simulators running together")
        for name in QAPeople.names {
            let email = QAPeople.email(for: name)
            let domain = email.split(separator: "@").last.map(String.init) ?? ""
            XCTAssertTrue(QAPeople.emailDomains.contains(domain), email)
            XCTAssertFalse(QAScreenText.showsTestWording(email), email)
        }
    }
}
