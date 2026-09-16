import Testing

@testable import DinnerOS

/// The one-tap week presets: what each tile reads from the week, and what it sets.
struct AutopilotWeekPresetTests {
    @Test func busyTileSetsAndClearsBusy() {
        var draft = AutopilotWeekContextDraft()
        #expect(AutopilotWeekPreset.allCases.allSatisfy { !draft.isOn($0) })

        draft.toggle(.busy, guestServings: 6)
        #expect(draft.busy)
        #expect(draft.isOn(.busy))
        #expect(!draft.isOn(.guests))

        draft.toggle(.busy, guestServings: 6)
        #expect(!draft.busy)
        #expect(draft.isEmpty)
    }

    @Test func guestsTileSetsTheWeeksServings() {
        var draft = AutopilotWeekContextDraft()
        draft.toggle(.guests, guestServings: 6)

        #expect(draft.servings == 6)
        #expect(draft.isOn(.guests))

        draft.toggle(.guests, guestServings: 6)
        #expect(draft.servings == nil)
        #expect(!draft.isOn(.guests))
    }

    /// Guests for one night counts as guests, and turning the tile off clears those days
    /// too, so the tile never reads "off" while a day still serves extra.
    @Test func guestsTileCoversPerDayServings() {
        var draft = AutopilotWeekContextDraft()
        draft[.fri].servings = 6
        #expect(draft.isOn(.guests))

        draft.toggle(.guests, guestServings: 8)
        #expect(draft.servings == nil)
        #expect(draft[.fri].servings == nil)
        #expect(!draft.isOn(.guests))
        #expect(draft.isEmpty)
    }

    /// A day that also skips keeps its skip when the guests tile clears its servings.
    @Test func guestsTileKeepsOtherDaySettings() {
        var draft = AutopilotWeekContextDraft()
        draft[.sat].skip = true
        draft[.fri].servings = 6

        draft.toggle(.guests, guestServings: 6)
        #expect(draft[.sat].skip)
        #expect(draft[.fri].isEmpty)
        #expect(!draft.isEmpty)
    }

    @Test func awayIsExclusive() {
        var draft = AutopilotWeekContextDraft()
        draft.toggle(.busy, guestServings: 6)
        draft.toggle(.guests, guestServings: 6)

        draft.toggle(.away, guestServings: 6)
        #expect(draft.skip)
        #expect(draft.isOn(.away))
        // Busy and guests read as off while the week is skipped, whatever is stored.
        #expect(!draft.isOn(.busy))
        #expect(!draft.isOn(.guests))

        draft.toggle(.busy, guestServings: 6)
        #expect(!draft.skip)
        #expect(draft.isOn(.busy))
    }

    /// Turning Away off again through its own tile leaves the week as it was.
    @Test func awayTileTurnsItselfOff() {
        var draft = AutopilotWeekContextDraft()
        draft.toggle(.away, guestServings: 6)
        draft.toggle(.away, guestServings: 6)

        #expect(!draft.skip)
        #expect(draft.isEmpty)
    }
}
