import XCTest

/// Temporary (agent B, build 121): scrolls the Menu down and up ten times, writing how long each
/// swipe took, with a rest after each so CPU can be read from outside. Not part of the harness.
final class AgentBMenuScrollQATests: XCTestCase {
    override func setUp() {
        continueAfterFailure = true
        executionTimeAllowance = 600
    }

    @MainActor
    func testScrollMenuDownAndUp() throws {
        let dir = ProcessInfo.processInfo.environment["AGENT_B_SHOTS"] ?? NSTemporaryDirectory()
        let app = launchSignedIn()
        sleep(3)
        var lines: [String] = []
        for i in 1...10 {
            for direction in ["down", "up"] {
                let start = Date()
                for _ in 0..<3 {
                    if direction == "down" { app.swipeUp() } else { scrollBack(app) }
                }
                lines.append(String(format: "%@ %d %@ %.1fs", "\(Date())", i, direction, Date().timeIntervalSince(start)))
                try? lines.joined(separator: "\n").write(
                    toFile: (dir as NSString).appendingPathComponent("menuscroll.txt"), atomically: true,
                    encoding: .utf8)
                sleep(3)
            }
        }
    }
}
