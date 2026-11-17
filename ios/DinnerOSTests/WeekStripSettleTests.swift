import Testing

@testable import DinnerOS

/// Bringing the selected week into view after the strip appears. A strip that left the screen
/// must never be scrolled again: SwiftUI's scrollTo crashed on one (a crash report from a QA run,
/// in `WeekStrip.pills`).
@MainActor
struct WeekStripSettleTests {
    @Test func scrollsUntilTheWeekShows() async {
        var scrolls = 0
        let finished = await WeekStripSettle.run(pause: .milliseconds(1), isDone: { scrolls >= 3 }) { scrolls += 1 }
        #expect(finished)
        #expect(scrolls == 3)
    }

    @Test func givesUpAfterItsTries() async {
        var scrolls = 0
        let finished = await WeekStripSettle.run(tries: 4, pause: .milliseconds(1), isDone: { false }) { scrolls += 1 }
        #expect(finished)
        #expect(scrolls == 5)
    }

    @Test func stopsScrollingOnceTheStripIsGone() async {
        var scrolls = 0
        let task = Task { @MainActor in
            await WeekStripSettle.run(pause: .milliseconds(200), isDone: { false }) { scrolls += 1 }
        }
        try? await Task.sleep(for: .milliseconds(50))
        task.cancel()
        let finished = await task.value
        #expect(!finished)
        #expect(scrolls == 1)
    }

    @Test func neverScrollsWhenAlreadyCancelled() async {
        var scrolls = 0
        let task = Task { @MainActor in
            withUnsafeCurrentTask { $0?.cancel() }
            return await WeekStripSettle.run(pause: .milliseconds(1), isDone: { false }) { scrolls += 1 }
        }
        #expect(await task.value == false)
        #expect(scrolls == 0)
    }
}
