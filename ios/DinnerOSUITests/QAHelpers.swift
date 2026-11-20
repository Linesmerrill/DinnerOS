import UIKit
import XCTest

/// When the screenshot settle step last asked SpringBoard about banners.
@MainActor enum QASettle {
    static var lastBannerCheck = Date.distantPast
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
        app.launch()
        assertFullScreen(app)
        let devSignIn = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Developer sign-in'")).firstMatch
        if devSignIn.waitForExistence(timeout: 8) {
            devSignIn.tap()
        }
        // Shown once after a fresh install to a household with Plus on the house.
        if app.buttons["Thanks"].waitForExistence(timeout: 4) { app.buttons["Thanks"].tap() }
        XCTAssertTrue(app.buttons["Week Menu"].waitForExistence(timeout: 30), "The Menu tab didn’t open after sign-in")
        return app
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
        let frame = window.frame
        if frame.width < screen.width - 2 || frame.height < screen.height - 2 {
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
    @MainActor
    func snap(_ app: XCUIApplication, _ name: String, settle: Bool = true) {
        if settle { settleForScreenshot(app) }
        let screenshot = XCUIScreen.main.screenshot()
        let orientation = XCUIDevice.shared.orientation
        // Only a device on its side needs the pixels redrawn; upright ones are kept as taken.
        let attachment =
            orientation == .landscapeLeft || orientation == .landscapeRight
            ? XCTAttachment(data: Self.uprightPNG(screenshot.image), uniformTypeIdentifier: "public.png")
            : XCTAttachment(screenshot: screenshot, quality: .original)
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
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
