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
