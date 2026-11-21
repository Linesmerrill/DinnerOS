import Testing

@testable import DinnerOS

/// Screenshot QA can render channel-dependent screens as an App Store build (build 121), and
/// the tester thanks never shows there.
struct BuildChannelOverrideTests {
    @Test func qaNamesAChannel() {
        #expect(BuildChannelStore.forced("appStore") == .appStore)
        #expect(BuildChannelStore.forced("testFlight") == .testFlight)
        #expect(BuildChannelStore.forced("development") == .development)
        #expect(BuildChannelStore.forced(nil) == nil)
        #expect(BuildChannelStore.forced("prod") == nil)
    }

    @Test func anAppStoreBuildNeverThanksTesters() {
        #expect(!BuildChannel.showsTesterThanks(.appStore, comp: nil))
        #expect(!BuildChannel.showsTesterThanks(nil, comp: nil))
    }
}
