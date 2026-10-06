import SwiftUI
import Testing

@testable import DinnerOS

struct PlanRefreshTests {
    /// Back from the background reloads, though the trip passes through inactive; launch and a
    /// pass through inactive alone don't.
    @MainActor @Test func reloadsOnlyWhenComingBackToTheApp() {
        let tracker = PlanRefresh.Tracker()
        #expect(!tracker.phaseChanged(to: .active))  // launch
        #expect(!tracker.phaseChanged(to: .inactive))  // Control Center
        #expect(!tracker.phaseChanged(to: .active))
        #expect(!tracker.phaseChanged(to: .inactive))
        #expect(!tracker.phaseChanged(to: .background))
        #expect(!tracker.phaseChanged(to: .inactive))
        #expect(tracker.phaseChanged(to: .active))  // back from the Home Screen
        #expect(!tracker.phaseChanged(to: .inactive))
        #expect(!tracker.phaseChanged(to: .active))  // once per return
    }

    /// A kitchen iPad left on the Menu picks up a phone's change within a couple of minutes.
    @Test func theMenuReloadsEveryTwoMinutes() {
        #expect(PlanRefresh.interval == .seconds(120))
    }

    /// Cancelling the pull doesn't cancel the reload it started.
    @Test func aCancelledPullStillFinishesTheReload() async {
        let finished = Flag()
        let pull = Task {
            await PlanRefresh.uncancellable {
                try? await Task.sleep(for: .milliseconds(50))
                await finished.set()
            }
        }
        pull.cancel()
        await pull.value
        #expect(await finished.value)
    }
}

private actor Flag {
    private(set) var value = false
    func set() { value = true }
}
