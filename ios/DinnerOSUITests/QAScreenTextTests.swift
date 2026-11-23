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
}
