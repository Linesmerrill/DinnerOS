import XCTest

/// The fade checks' own logic, with no app: reading the covers the app reports, and what counts
/// as a row resting part way under one.
final class FadeQAChecksTests: XCTestCase {
    private let screen = CGRect(x: 0, y: 0, width: 400, height: 800)
    /// A pinned picker and its fade at the top; a bottom bar's fade, the bar and the tab bar.
    private let covers = [
        FadeQAChecks.Cover(isFade: false, frame: CGRect(x: 0, y: 100, width: 400, height: 50)),
        FadeQAChecks.Cover(isFade: true, frame: CGRect(x: 0, y: 150, width: 400, height: 10)),
        FadeQAChecks.Cover(isFade: true, frame: CGRect(x: 0, y: 628, width: 400, height: 32)),
        FadeQAChecks.Cover(isFade: false, frame: CGRect(x: 0, y: 660, width: 400, height: 60)),
        FadeQAChecks.Cover(isFade: false, frame: CGRect(x: 0, y: 720, width: 400, height: 80)),
    ]

    func testTheReportedCoversReadBack() {
        let covers = FadeQAChecks.decode("b:0.0,100.0,400.0,60.0;f:0.0,660.0,400.0,32.5")
        XCTAssertEqual(
            covers,
            [
                FadeQAChecks.Cover(isFade: false, frame: CGRect(x: 0, y: 100, width: 400, height: 60)),
                FadeQAChecks.Cover(isFade: true, frame: CGRect(x: 0, y: 660, width: 400, height: 32.5)),
            ])
        XCTAssertEqual(FadeQAChecks.decode(""), [])
        XCTAssertEqual(FadeQAChecks.decode("f:1,2,3;x:1,2,3,4"), [])
    }

    func testARowClearOfEveryCoverPasses() {
        let rows = [("Calories", CGRect(x: 16, y: 170, width: 368, height: 40))]
        XCTAssertTrue(FadeQAChecks.halfCovered(rows, covers: covers, screen: screen).isEmpty)
    }

    func testARowRestingOnAFadesEdgeIsClear() {
        let rows = [("Calories", CGRect(x: 16, y: 159, width: 368, height: 40))]
        XCTAssertTrue(FadeQAChecks.halfCovered(rows, covers: covers, screen: screen).isEmpty)
    }

    /// The Duo's Nutrition tab: Calories half under the pinned picker's fade.
    func testARowHalfUnderThePickersFadeFails() {
        let rows = [("Calories", CGRect(x: 16, y: 140, width: 368, height: 40))]
        XCTAssertEqual(FadeQAChecks.halfCovered(rows, covers: covers, screen: screen).map(\.label), ["Calories"])
    }

    /// Sign Out washed out above the tab bar.
    func testARowHalfInTheBottomFadeFails() {
        let rows = [("Sign Out", CGRect(x: 16, y: 640, width: 368, height: 44))]
        XCTAssertEqual(FadeQAChecks.halfCovered(rows, covers: covers, screen: screen).map(\.label), ["Sign Out"])
        XCTAssertTrue(FadeQAChecks.halfCovered(rows, covers: covers, screen: screen, edges: .top).isEmpty)
    }

    /// A bar's own buttons, and a row scrolled wholly under bars, aren't resting under a fade.
    func testWhollyUnderABarIsFine() {
        let rows = [
            ("Nutrition", CGRect(x: 300, y: 110, width: 90, height: 32)),
            ("Menu", CGRect(x: 20, y: 730, width: 80, height: 50)),
            ("Open in Walmart", CGRect(x: 16, y: 670, width: 368, height: 44)),
            ("Fat", CGRect(x: 16, y: 60, width: 368, height: 60)),
        ]
        let found = FadeQAChecks.halfCovered(rows, covers: covers, screen: screen)
        // The row going under the picker shows below the bar's edge: cut, so it counts.
        XCTAssertEqual(found.map(\.label), ["Fat"])
    }

    /// Add Prices ghosted: wholly inside the fade above the Walmart note, it still shows faintly.
    func testARowWhollyInsideAFadeFails() {
        let rows = [("Add Prices", CGRect(x: 16, y: 632, width: 368, height: 24))]
        XCTAssertEqual(FadeQAChecks.halfCovered(rows, covers: covers, screen: screen).map(\.label), ["Add Prices"])
    }

    func testOnlyWhatsOnScreenCounts() {
        let rows = [("Delete Account", CGRect(x: 16, y: 790, width: 368, height: 44))]
        XCTAssertTrue(FadeQAChecks.halfCovered(rows, covers: covers, screen: screen).isEmpty)
    }

    func testTheCoverOverARowIsNamed() {
        let frame = CGRect(x: 16, y: 640, width: 368, height: 30)
        XCTAssertEqual(FadeQAChecks.cover(over: frame, in: covers), covers[2])
        XCTAssertNil(FadeQAChecks.cover(over: CGRect(x: 16, y: 300, width: 368, height: 30), in: covers))
    }
}
