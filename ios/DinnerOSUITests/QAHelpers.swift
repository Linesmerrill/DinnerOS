import UIKit
import XCTest

/// When the screenshot settle step last asked SpringBoard about banners.
@MainActor enum QASettle {
    static var lastBannerCheck = Date.distantPast
}

/// A screenshot reduced to one pixel per point, for telling two shots apart.
struct QAShotImage {
    let width: Int
    let height: Int
    /// RGBX, a row at a time from the top.
    let pixels: [UInt8]
    /// Per 8-point cell, row by row: whether it lies clear of the status bar and home indicator.
    let cellKept: [Bool]
    let columns: Int
    /// Per cell, its average red, green and blue, for a quick first look.
    let cellMeans: [Double]
}

/// Tells whether two screenshots show the same screen (Hark's review, build 122 item 13a).
///
/// Two shots of one screen used to pass because the clock had moved on. Here the status bar and
/// the home indicator are left out, the screen is cut into 8-point cells, and two shots count as
/// the same when fewer than 1% of the cells changed (a cell changes when any pixel's color moves
/// by more than 24 of 255). Cells rather than pixels: a menu or a household name changes few
/// pixels (thin text on white, under 1%) but many cells (3% or more), while the same screen
/// changes none, or a few under the toolbar's glass. scripts/qa_shot_compare.py does the same
/// before publishing; keep the two in step.
enum QAShotCompare {
    static let sameScreen = 0.01
    static let channelTolerance = 24
    static let cell = 8

    /// Points to leave out: the status bar (top), the home indicator (bottom) and, on the iPhone
    /// Duo cover screen, the status bar at the top of the right-hand rail (x from 382, 160 tall).
    static func mask(width: Int, height: Int, pad: Bool) -> (top: Int, bottom: Int, rail: (x: Int, height: Int)?) {
        if pad { return (24, 20, nil) }
        if width == 466 && height == 678 { return (0, 0, (382, 160)) }
        if max(width, height) <= 667 { return (20, 0, nil) }
        return (62, 34, nil)
    }

    /// The PNG at one pixel per point (`scale` pixels per point in the PNG), sampled, not blended.
    static func image(png: Data, scale: CGFloat, pad: Bool) -> QAShotImage? {
        guard let source = UIImage(data: png)?.cgImage, scale > 0 else { return nil }
        let width = Int((CGFloat(source.width) / scale).rounded())
        let height = Int((CGFloat(source.height) / scale).rounded())
        guard width > 0, height > 0 else { return nil }
        var pixels = [UInt8](repeating: 0, count: width * height * 4)
        let drawn = pixels.withUnsafeMutableBytes { buffer -> Bool in
            guard
                let context = CGContext(
                    data: buffer.baseAddress, width: width, height: height, bitsPerComponent: 8,
                    bytesPerRow: width * 4, space: CGColorSpaceCreateDeviceRGB(),
                    bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue)
            else { return false }
            context.interpolationQuality = .none
            context.draw(source, in: CGRect(x: 0, y: 0, width: width, height: height))
            return true
        }
        guard drawn else { return nil }
        return QAShotImage(
            width: width, height: height, pixels: pixels,
            cellKept: keptCells(width: width, height: height, mask: mask(width: width, height: height, pad: pad)),
            columns: width / cell, cellMeans: means(pixels, width: width, height: height))
    }

    /// Each whole cell's average color, three values a cell, row by row.
    static func means(_ pixels: [UInt8], width: Int, height: Int) -> [Double] {
        let columns = width / cell
        let rows = height / cell
        var sums = [Double](repeating: 0, count: columns * rows * 3)
        for y in 0..<(rows * cell) {
            let rowBase = (y / cell) * columns
            for x in 0..<(columns * cell) {
                let p = (y * width + x) * 4
                let c = (rowBase + x / cell) * 3
                sums[c] += Double(pixels[p])
                sums[c + 1] += Double(pixels[p + 1])
                sums[c + 2] += Double(pixels[p + 2])
            }
        }
        let area = Double(cell * cell)
        return sums.map { $0 / area }
    }

    /// How far apart the darkest and lightest points inside `rect` (in points) are, 0 to 255 of
    /// brightness: text drawn whole is far from its background, a row faded out isn't there at
    /// all (Hark's review, build 122 round 5: Step 1 faded out on the iPad cooking screen).
    static func brightnessRange(_ image: QAShotImage, in rect: CGRect) -> Int {
        let minX = max(Int(rect.minX.rounded(.up)), 0)
        let maxX = min(Int(rect.maxX.rounded(.down)), image.width)
        let minY = max(Int(rect.minY.rounded(.up)), 0)
        let maxY = min(Int(rect.maxY.rounded(.down)), image.height)
        guard minX < maxX, minY < maxY else { return 0 }
        var darkest = 255
        var lightest = 0
        for y in minY..<maxY {
            for x in minX..<maxX {
                let p = (y * image.width + x) * 4
                let r = Int(image.pixels[p])
                let g = Int(image.pixels[p + 1])
                let b = Int(image.pixels[p + 2])
                let brightness = (299 * r + 587 * g + 114 * b) / 1000
                darkest = min(darkest, brightness)
                lightest = max(lightest, brightness)
            }
        }
        return lightest - darkest
    }

    /// True when the cell averages alone prove at least `share` of the cells changed: a cell whose
    /// average moved by more than the tolerance in a channel has a pixel that did too. Cheap, so
    /// most pairs of different screens never need the pixel by pixel look.
    static func surelyDifferent(_ a: QAShotImage, _ b: QAShotImage, share: Double) -> Bool {
        guard a.cellMeans.count == b.cellMeans.count else { return true }
        let total = a.cellKept.filter { $0 }.count
        guard total > 0 else { return false }
        var moved = 0
        for index in a.cellKept.indices where a.cellKept[index] {
            let c = index * 3
            let shift = max(
                abs(a.cellMeans[c] - b.cellMeans[c]), abs(a.cellMeans[c + 1] - b.cellMeans[c + 1]),
                abs(a.cellMeans[c + 2] - b.cellMeans[c + 2]))
            if shift > Double(channelTolerance) { moved += 1 }
        }
        return Double(moved) / Double(total) >= share
    }

    /// Which whole cells lie clear of the masked areas.
    static func keptCells(width: Int, height: Int, mask: (top: Int, bottom: Int, rail: (x: Int, height: Int)?))
        -> [Bool]
    {
        let columns = width / cell
        let rows = height / cell
        var kept = [Bool](repeating: true, count: columns * rows)
        for row in 0..<rows {
            let top = row * cell
            let bottom = top + cell
            for column in 0..<columns {
                let left = column * cell
                let right = left + cell
                let inStatusBar = top < mask.top
                let inHomeIndicator = bottom > height - mask.bottom
                let inRail = mask.rail.map { right > $0.x && top < $0.height } ?? false
                if inStatusBar || inHomeIndicator || inRail { kept[row * columns + column] = false }
            }
        }
        return kept
    }

    /// The share of kept cells (0 to 1) in which some pixel changed; 1 for shots of different sizes.
    /// Stops counting once the share passes `enough`.
    static func changedShare(_ a: QAShotImage, _ b: QAShotImage, enough: Double = 1) -> Double {
        guard a.width == b.width, a.height == b.height else { return 1 }
        let total = a.cellKept.filter { $0 }.count
        guard total > 0 else { return 0 }
        let limit = Int((enough * Double(total)).rounded(.up))
        var changed = [Bool](repeating: false, count: a.cellKept.count)
        var count = 0
        let rows = a.height / cell
        a.pixels.withUnsafeBufferPointer { pa in
            b.pixels.withUnsafeBufferPointer { pb in
                for y in 0..<(rows * cell) {
                    let rowBase = (y / cell) * a.columns
                    for x in 0..<(a.columns * cell) {
                        let index = rowBase + x / cell
                        guard a.cellKept[index], !changed[index] else { continue }
                        let p = (y * a.width + x) * 4
                        let moved =
                            max(
                                abs(Int(pa[p]) - Int(pb[p])), abs(Int(pa[p + 1]) - Int(pb[p + 1])),
                                abs(Int(pa[p + 2]) - Int(pb[p + 2]))) > channelTolerance
                        if moved {
                            changed[index] = true
                            count += 1
                            if count >= limit { return }
                        }
                    }
                }
            }
        }
        return Double(count) / Double(total)
    }

    /// Pairs of screens, by checklist name as published (`slug`), that are different screens
    /// though under 1% of cells may change: on an iPad the search for one recipe and for another
    /// show one result card each in a wide, mostly empty page; the Household tab of a household
    /// without Plus differs from the main test household's only in its name, initials and Plus
    /// line (build 122 round 4, Hark's item 15c). scripts/qa_shot_compare.py ALLOWED_PAIRS is the
    /// same list.
    static let allowedPairs: [String: Set<String>] = [
        "Menu search, one card each": ["menu-search-typed-imported", "menu-search-typed-typed-by-hand"],
        "Household with and without Plus": ["household-new-household", "household"],
        "iPad cooking screen at its top, from either way in": ["start-cooking-steps", "cooking-imported"],
        "Pantry with Butter in view": ["pantry", "pantry-scrolled-to-butter"],
        "Shop with the flour in view": ["shop", "shop-scrolled-to-flour"],
    ]

    /// The published file name for a checklist name, as scripts/qa-screenshots.py makes it:
    /// "Menu › search typed, imported (light)" -> "menu-search-typed-imported".
    static func slug(_ name: String) -> String {
        var screen = name
        for mode in [" (light)", " (dark)"] where screen.hasSuffix(mode) {
            screen = String(screen.dropLast(mode.count))
        }
        var out = ""
        var pendingDash = false
        for scalar in screen.lowercased().unicodeScalars {
            if ("a"..."z").contains(scalar) || ("0"..."9").contains(scalar) {
                if pendingDash, !out.isEmpty { out.append("-") }
                out.unicodeScalars.append(scalar)
                pendingDash = false
            } else {
                pendingDash = true
            }
        }
        return String(out.prefix(80))
    }

    /// Whether two checklist names are an allowed look-alike pair.
    static func isAllowedPair(_ first: String, _ second: String) -> Bool {
        let a = slug(first)
        let b = slug(second)
        return a != b && allowedPairs.values.contains([a, b])
    }

    static func isSameScreen(_ a: QAShotImage, _ b: QAShotImage) -> Bool {
        !surelyDifferent(a, b, share: sameScreen) && changedShare(a, b, enough: sameScreen) < sameScreen
    }
}

/// The screenshots this test run has kept, to refuse a second copy of a screen.
@MainActor enum QAShots {
    static var kept: [(name: String, image: QAShotImage)] = []
}

/// Test wording on screen (Hark's build 122 round 5, items 6 and 7): a screenshot that shows "QA",
/// "Test" or "Tester" as a word of its own, in a dinner, household or person's name, fails its
/// test. Whole words and case kept, so "Contest", "Testing" and "TestFlight" pass.
enum QAScreenText {
    static let words = ["QA", "Test", "Tester"]

    private static let pattern: NSRegularExpression = {
        let alternatives = words.map(NSRegularExpression.escapedPattern(for:)).joined(separator: "|")
        // swiftlint:disable:next force_try
        return try! NSRegularExpression(pattern: "(?<![\\p{L}\\p{N}])(?:\(alternatives))(?![\\p{L}\\p{N}])")
    }()

    /// Whether a label or value shows one of the words on its own.
    static func showsTestWording(_ text: String) -> Bool {
        pattern.firstMatch(in: text, range: NSRange(text.startIndex..., in: text)) != nil
    }

    /// The texts that show test wording, each once, in order.
    static func offending(_ texts: [String]) -> [String] {
        var seen = Set<String>()
        return texts.filter { showsTestWording($0) && seen.insert($0).inserted }
    }
}

/// Realistic people for the accounts UI tests make (build 123): an avatar or a member list shows
/// "Maya Rivera", never "Qa New 12345".
enum QAPeople {
    static let names = [
        "Jordan Avery", "Maya Rivera", "Sam Okafor", "Priya Natarajan", "Elliot Chen", "Rosa Delgado",
        "Noah Bennett", "Hannah Kim", "Marcus Hale", "Lena Fischer", "Owen Price", "Aisha Rahman",
    ]

    /// A new person's developer subject, unique per run: "maya-rivera-48213". The app shows it
    /// as "Maya Rivera" (DeveloperIdentity leaves the number out).
    static func subject(_ name: String? = nil) -> String {
        let person = name ?? names.randomElement()!
        return person.lowercased().replacingOccurrences(of: " ", with: "-") + "-\(Int.random(in: 10000...99999))"
    }

    /// The domains an email for a screenshot person can be on: kept for examples (RFC 2606), so
    /// never a real person's inbox, and ones the development API's email accounts accept.
    static let emailDomains = ["example.com", "example.net", "example.org"]

    /// A realistic email for a person, with no run number in it (Hark's build 121 review, item 8:
    /// "hannah.kim395@example.test" read as a test account): "hannah.j.kim@example.com",
    /// "hannahjkim@example.net" or "hjkim@example.org". The middle initial, the form and the
    /// domain are picked at random, about 160 emails per name, so simulators running together
    /// almost never share one; a repeat is the same person again, which every test allows.
    static func email(for name: String) -> String {
        let words = name.lowercased().split(separator: " ").map(String.init)
        let first = words.first ?? "sam"
        let last = words.count > 1 ? words[words.count - 1] : "lee"
        let initial = String("abcdefghjklmnprstw".randomElement()!)
        let local = [
            "\(first).\(initial).\(last)", "\(first)\(initial)\(last)", "\(first.prefix(1))\(initial)\(last)",
        ].randomElement()!
        return "\(local)@\(emailDomains.randomElement()!)"
    }

    /// The name the app gives a subject: its words without the numbers, capitalized.
    static func displayName(forSubject subject: String) -> String {
        subject.split(whereSeparator: { !$0.isLetter && !$0.isNumber })
            .filter { !$0.allSatisfy(\.isNumber) }
            .map { $0.capitalized }
            .joined(separator: " ")
    }

    /// "Rivera Kitchen" for "maya-rivera-48213".
    static func kitchen(forSubject subject: String) -> String {
        let last = displayName(forSubject: subject).split(separator: " ").last.map(String.init) ?? "Our"
        return "\(last) Kitchen"
    }
}

/// A screenshot taken but not yet kept: its PNG and its reduced copy for comparing.
struct QAShot {
    let data: Data
    let image: QAShotImage?
    /// Visible labels and values that show test wording (QAScreenText) when it was taken.
    var testWording: [String] = []

    func isSameScreen(as other: QAShot) -> Bool {
        guard let image, let otherImage = other.image else { return data == other.data }
        return QAShotCompare.isSameScreen(image, otherImage)
    }
}

/// Shared steps for the QA runs: sign in on the development server, wait for things, and keep a
/// named screenshot of every screen for the checklist.
extension XCTestCase {
    @MainActor
    func launchSignedIn(dark: Bool = false) -> XCUIApplication {
        // Light or dark comes from the simulator: run `xcrun simctl ui <device> appearance dark`
        // before a dark run. Neither a launch argument nor XCUIDevice.appearance took effect on
        // iOS 26.2, and the device setting could stick until a reboot.
        _ = dark
        let app = XCUIApplication()
        applyOrientation()
        for (key, value) in ProcessInfo.processInfo.environment where key.hasPrefix("QA_APP_") {
            app.launchEnvironment[String(key.dropFirst("QA_APP_".count))] = value
        }
        // Lets the tests see a photo showing its no-photo art (`QAMarkers`, build 123).
        app.launchEnvironment["QA_MARKERS"] = "1"
        app.launch()
        assertFullScreen(app)
        let devSignIn = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Developer sign-in'")).firstMatch
        if devSignIn.waitForExistence(timeout: 8) {
            devSignIn.tap()
        }
        // Shown once after a fresh install to a household with Plus on the house.
        if app.buttons["Thanks"].waitForExistence(timeout: 4) { app.buttons["Thanks"].tap() }
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 30), "The Menu tab didn’t open after sign-in")
        closeNewsBanner(app)
        return app
    }

    /// Closes the news banner ("2 invites counted") if one shows: it's news for the shared test
    /// household, not part of the screen under test, and on an iPhone SE it takes room the
    /// screen's content needs (build 121, Hark's review item 4).
    @MainActor
    func closeNewsBanner(_ app: XCUIApplication) {
        let banner = app.descendants(matching: .any)["NewsBanner"].firstMatch
        guard banner.waitForExistence(timeout: 2) else { return }
        let close = banner.buttons["Close"].firstMatch
        if close.exists { close.tap() }
        _ = waitGone(banner, timeout: 3)
    }

    /// iPadOS 26 can switch a simulator to windowed apps, and then every test runs in a small
    /// window (build 121). Says so plainly in the log (QA-WINDOWED), so the matrix can put the
    /// simulator back on Full Screen Apps (QASetupTests) and retry.
    @MainActor
    func assertFullScreen(_ app: XCUIApplication) {
        guard UIDevice.current.userInterfaceIdiom == .pad else { return }
        let screen = XCUIApplication(bundleIdentifier: "com.apple.springboard").frame
        let window = app.windows.firstMatch
        guard window.waitForExistence(timeout: 10), !screen.isEmpty else { return }
        // SpringBoard reports its frame upright even when the iPad is on its side: compare the
        // longer and shorter sides, not width with width.
        let frame = window.frame
        let (long, short) = (max(frame.width, frame.height), min(frame.width, frame.height))
        let (screenLong, screenShort) = (max(screen.width, screen.height), min(screen.width, screen.height))
        if long < screenLong - 2 || short < screenShort - 2 {
            print("QA-WINDOWED: the app is a \(Int(frame.width))x\(Int(frame.height)) window")
            XCTFail("The iPad is in windowed apps mode (QA-WINDOWED); QASetupTests puts it back")
        }
    }

    /// Portrait unless the run sets QA_ORIENTATION=landscape (the kitchen iPad lives on a
    /// stand in landscape).
    @MainActor
    func applyOrientation() {
        let landscape = ProcessInfo.processInfo.environment["QA_ORIENTATION"] == "landscape"
        XCUIDevice.shared.orientation = landscape ? .landscapeLeft : .portrait
    }

    /// The way back from a pushed page: the navigation bar's first button, or, on iPhone Duo's
    /// cover screen (iOS 27), the system back button, which sits in the column beside the page
    /// instead of in the bar.
    @MainActor
    func backButton(_ app: XCUIApplication) -> XCUIElement {
        // The system back button first, wherever it sits: on the Duo the bar's first button is
        // the page's own (Join, Start Cooking).
        let system = app.buttons["BackButton"].firstMatch
        if system.exists { return system }
        return app.navigationBars.buttons.firstMatch
    }

    /// Keeps a screenshot in the test results, named for the checklist. First the screen
    /// settles (no spinner, no keyboard tip, no system banner or toast); then it's kept as an
    /// upright PNG: an iPad in landscape once came out as a portrait image tagged with an EXIF
    /// rotation and cut off (build 120), so the pixels are redrawn the right way up.
    ///
    /// A screen this run already kept (status bar and home indicator aside, under 1% changed) fails
    /// the test and isn't kept again (Hark's review, build 122 item 13a). `unique: false` is for a
    /// shot of what went wrong, kept beside a failure: it may well be a screen seen before.
    @MainActor
    @discardableResult
    func snap(_ app: XCUIApplication, _ name: String, settle: Bool = true, unique: Bool = true) -> Data {
        let shot = capture(app, settle: settle)
        keep(shot, name, unique: unique)
        return shot.data
    }

    /// Whether this test's screenshots go to Hark (scripts/qa-screenshots.py TESTS).
    var publishesScreenshots: Bool {
        let published = ["AppSweepQATests", "ScreensQATests", "StartCookingQATests"]
        return published.contains(String(describing: type(of: self))) || name.contains("testSignInScreenWithFaceID")
    }

    /// Takes a screenshot without keeping it, to compare before deciding.
    @MainActor
    func capture(_ app: XCUIApplication, settle: Bool = true) -> QAShot {
        if settle { settleForScreenshot(app) }
        let screenshot = XCUIScreen.main.screenshot()
        let orientation = XCUIDevice.shared.orientation
        // Only a device on its side needs the pixels redrawn; upright ones are kept as taken.
        let data =
            orientation == .landscapeLeft || orientation == .landscapeRight
            ? Self.uprightPNG(screenshot.image) : screenshot.pngRepresentation
        let pad = UIDevice.current.userInterfaceIdiom == .pad
        return QAShot(
            data: data, image: QAShotCompare.image(png: data, scale: screenshot.image.scale, pad: pad),
            testWording: testWording(on: app))
    }

    /// The labels and values on screen that show test wording ("QA Beef Tacos"): text, buttons
    /// and fields, from one snapshot of the app, so it stays quick.
    @MainActor
    func testWording(on app: XCUIApplication) -> [String] {
        guard let root = try? app.snapshot() else { return [] }
        let screen = root.frame
        let kinds: Set<XCUIElement.ElementType> = [.staticText, .button, .textField, .searchField, .textView, .link]
        var texts: [String] = []
        var stack = [root]
        while let element = stack.popLast() {
            stack.append(contentsOf: element.children)
            guard kinds.contains(element.elementType) else { continue }
            let frame = element.frame
            guard !frame.isEmpty, screen.intersects(frame) else { continue }
            texts.append(element.label)
            if let value = element.value as? String { texts.append(value) }
        }
        return QAScreenText.offending(texts)
    }

    /// Fails the test when a screenshot shows test wording, naming the shot and the text.
    @MainActor
    func failOnTestWording(_ wording: [String], in name: String) {
        guard !wording.isEmpty else { return }
        let shown = wording.prefix(5).map { "“\($0)”" }.joined(separator: ", ")
        XCTFail("QA-TEST-WORDING: “\(name)” shows test wording: \(shown). Use a realistic name.")
    }

    /// Keeps a captured screenshot in the test results under its checklist name, unless it shows
    /// a screen already kept: then the test fails, naming both.
    ///
    /// Only the tests whose screenshots are published are held to this (the publisher's list,
    /// scripts/qa-screenshots.py TESTS). A flow test may come back to where it began on purpose
    /// (CompanionQATests: the list after a delete is the list it started from).
    @MainActor
    func keep(_ shot: QAShot, _ name: String, unique: Bool = true) {
        if unique, publishesScreenshots, let image = shot.image {
            for earlier in QAShots.kept
            where !QAShotCompare.isAllowedPair(name, earlier.name) && QAShotCompare.isSameScreen(image, earlier.image) {
                let share = QAShotCompare.changedShare(image, earlier.image)
                XCTFail(
                    "QA-DUPLICATE: “\(name)” is the same screen as “\(earlier.name)” "
                        + "(\(String(format: "%.2f", share * 100))% changed)")
                return
            }
            QAShots.kept.append((name, image))
        }
        let attachment = XCTAttachment(data: shot.data, uniformTypeIdentifier: "public.png")
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        // Kept first, so the results show what was on screen. A shot of what went wrong
        // (`unique: false`) isn't held to it.
        if unique { failOnTestWording(shot.testWording, in: name) }
    }

    /// Keeps a "scrolled" screenshot only when the page moved: one the same as `before` (a page
    /// that fits on the screen) is left out, with a QA-SKIP line in the log, instead of
    /// publishing a copy (Hark's review, build 122 item 13c). Returns whether it was kept.
    @MainActor
    @discardableResult
    func keepIfScrolled(_ shot: QAShot, _ name: String, before: QAShot) -> Bool {
        if shot.isSameScreen(as: before) {
            print("QA-SKIP: “\(name)” left out: the page didn’t scroll (it fits on the screen)")
            return false
        }
        keep(shot, name)
        return true
    }

    /// The screenshot as PNG data with its rotation applied to the pixels and no EXIF tag.
    static func uprightPNG(_ image: UIImage) -> Data {
        let format = UIGraphicsImageRendererFormat()
        format.scale = image.scale
        format.opaque = true
        var size = image.size
        // The device is on its side but the image isn't: draw it in the orientation of the device.
        let orientation = XCUIDevice.shared.orientation
        let landscape = orientation == .landscapeLeft || orientation == .landscapeRight
        let needsTurn = landscape && size.height > size.width && image.imageOrientation == .up
        if needsTurn { size = CGSize(width: size.height, height: size.width) }
        let renderer = UIGraphicsImageRenderer(size: size, format: format)
        return renderer.pngData { context in
            if needsTurn, let cg = image.cgImage {
                // Turn a portrait framebuffer a quarter turn to match landscapeLeft (home button
                // on the right): counterclockwise for landscapeLeft, clockwise for landscapeRight.
                let turned = UIImage(
                    cgImage: cg, scale: image.scale, orientation: orientation == .landscapeLeft ? .left : .right)
                turned.draw(in: CGRect(origin: .zero, size: size))
            } else {
                image.draw(in: CGRect(origin: .zero, size: size))
            }
            _ = context
        }
    }

    /// Waits for the screen to be still before a screenshot: no system banner or keyboard tip left
    /// over (a keyboard tip, "Ready for Apple Intelligence"). Spinners aren't waited on here: the
    /// week strip keeps one ("Loading earlier weeks") off screen; tests wait for their content.
    @MainActor
    func settleForScreenshot(_ app: XCUIApplication) {
        // The keyboard's "Slide to type" tip has a Continue button (the matrix also turns the
        // tip off on every simulator).
        if app.keyboards.firstMatch.exists {
            let tip = app.buttons["Continue"].firstMatch
            if tip.exists, tip.isHittable { tip.tap() }
        }
        // System banners ("Ready for Apple Intelligence") sit in SpringBoard: swipe them up.
        // Asking SpringBoard is slow, so at most every 20 seconds.
        if Date().timeIntervalSince(QASettle.lastBannerCheck) > 20 {
            QASettle.lastBannerCheck = Date()
            let banner = XCUIApplication(bundleIdentifier: "com.apple.springboard")
                .otherElements["NotificationShortLookView"].firstMatch
            if banner.exists { banner.swipeUp() }
        }
        // Transitions and image fades finish.
        usleep(300_000)
    }

    @MainActor
    func waitGone(_ element: XCUIElement, timeout: TimeInterval = 5) -> Bool {
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: element)
        return XCTWaiter.wait(for: [gone], timeout: timeout) == .completed
    }

    /// The cooking screen for this recipe is up: its title heads the page and the cooking
    /// screen's own help button is in the bar (the recipe page has neither the button nor the
    /// walkthrough's title).
    @MainActor
    func openedCooking(_ recipe: String, in app: XCUIApplication) -> Bool {
        let help = app.buttons["How the Cooking Screen Works"].firstMatch
        guard help.waitForExistence(timeout: 10) else { return false }
        let title = app.staticTexts.matching(NSPredicate(format: "label == %@", recipe)).firstMatch
        return title.waitForExistence(timeout: 5) && !app.staticTexts["Welcome to Cooking with DinnerOS"].exists
    }

    /// The keyboard is up; tapping outside the field must put it away.
    @MainActor
    func assertKeyboardDismissesOnTapOutside(_ app: XCUIApplication, tapping target: XCUIElement, _ what: String) {
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 3), "No keyboard for \(what)")
        target.tap()
        XCTAssertTrue(waitGone(app.keyboards.firstMatch), "Keyboard stayed up after tapping outside \(what)")
    }

    /// Scrolls until the element is on screen.
    @MainActor
    func reveal(_ element: XCUIElement, in app: XCUIApplication, maxSwipes: Int = 8) {
        var swipes = 0
        // Hittable isn't enough: a pinned bottom bar (Open in Walmart, Add to Week) can sit over
        // an element XCTest still calls hittable. On an SE, Open in Walmart, its note and the tab
        // bar cover the bottom third, so keep it above that.
        // Judged by its top edge, so a tall section (a checklist) counts once its top is in the
        // upper half.
        func clear() -> Bool {
            // Judged by the frame, not isHittable: asking about a line of text in an iPad sheet
            // can fail the test outright ("Activation point invalid"). The top-half rule below
            // already keeps it clear of a pinned bottom bar.
            let screen = app.frame
            let frame = element.exists ? element.frame : .zero
            guard !frame.isEmpty, screen.contains(CGPoint(x: frame.midX, y: frame.midY)) else { return false }
            let top = element.frame.minY - screen.minY
            return top > screen.height * 0.08 && top < screen.height * 0.55
        }
        // A swipe can carry it from under the bar to under the top of the screen, or off it
        // altogether in a lazy list; then scroll back a little.
        func pastTheTop() -> Bool {
            element.exists && element.frame.minY < app.frame.minY + app.frame.height * 0.08
        }
        var seen = false
        while !clear(), swipes < maxSwipes {
            // Never seen after half the swipes: it's above, not below.
            let lookUp = !seen && !element.exists && swipes >= maxSwipes / 2
            if pastTheTop() || (seen && !element.exists) || lookUp {
                scrollBack(app)
            } else {
                seen = seen || element.exists
                app.swipeUp(velocity: .slow)
            }
            swipes += 1
        }
    }

    /// Waits for a page that was just pushed to finish arriving: its back button can be tapped
    /// and stays put across two looks 0.3 seconds apart. A shot taken mid push caught the round
    /// back button still turning into its final shape, a rectangle inside the circle (build 122
    /// round 4, Hark's item 7). Gives up after about 3 seconds; the shot is taken either way.
    @MainActor
    func waitForPushToSettle(_ app: XCUIApplication) {
        let back = backButton(app)
        let deadline = Date().addingTimeInterval(3)
        while Date() < deadline, !(back.exists && back.isHittable) { usleep(100_000) }
        var last = back.exists ? back.frame : .zero
        while Date() < deadline {
            usleep(300_000)
            let now = back.exists ? back.frame : .zero
            if now == last, !now.isEmpty { return }
            last = now
        }
    }

    /// Pulls a sheet down, or scrolls content back, with a drag that starts a third of the way
    /// down. Never `swipeDown()`: a fast downward swipe on iPadOS 26 can switch the iPad to
    /// windowed apps (build 121).
    @MainActor
    func pullDown(_ app: XCUIApplication) {
        let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3))
        let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.9))
        start.press(forDuration: 0.05, thenDragTo: end, withVelocity: .fast, thenHoldForDuration: 0)
    }

    /// Scrolls content back toward the top with a drag that starts mid-screen. A downward swipe
    /// on iPadOS 26 can turn a full-screen app into a small window, which then breaks typing.
    @MainActor
    func scrollBack(_ app: XCUIApplication) {
        let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.35))
        let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.75))
        start.press(forDuration: 0.05, thenDragTo: end)
    }
}
