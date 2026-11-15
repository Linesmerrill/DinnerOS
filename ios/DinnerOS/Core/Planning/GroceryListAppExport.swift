import Foundation

/// A grocery app outside DinnerOS that a week's list can be handed to in one action.
///
/// AnyList is the first. None of these apps publishes an API or a share extension that takes
/// items, so the handoff every one of them does document is the same: the whole list as plain
/// text, one item per line, pasted in one go. See `docs/shopping-providers.md` for what each
/// vendor actually documents.
///
/// A second app needs one value here and nothing else: the lines, the button, the guide, and
/// the confirmation all read from it.
nonisolated struct GroceryListApp: Identifiable, Equatable, Sendable {
    /// Stable: it's part of the `UserDefaults` key that remembers the explainer, so renaming
    /// the app later doesn't show the explainer a second time.
    let id: String
    /// The app's own name, as its button and messages say it.
    let name: String
    let systemImage: String
    /// Whether the app sorts pasted items into its own aisles.
    ///
    /// When it does, the aisle headers are left out: every pasted line becomes an item there,
    /// so a header would arrive as something to buy. The aisle *order* still survives, because
    /// the lines keep the order the Reminders export uses.
    let sortsIntoItsOwnAisles: Bool
    /// How to paste the list in the app, step by step, each with a picture of that screen.
    /// Shown every time the list is sent: the paste button is easy to miss.
    let guide: [GroceryListAppGuideStep]
    /// Opens the app. A bare launch: none of these apps takes items through a link.
    let launchURL: URL?
    /// The app's App Store page, for when it isn't installed.
    let appStoreURL: URL?

    /// AnyList's documented bulk import: one item per line, pasted into the Add Item field,
    /// categorized by AnyList on arrival.
    /// <https://help.anylist.com/articles/paste-items/>
    static let anyList = GroceryListApp(
        id: "anylist",
        name: "AnyList",
        systemImage: "checklist",
        sortsIntoItsOwnAisles: true,
        guide: [
            GroceryListAppGuideStep(
                imageName: "AnyListGuideStep1", text: "Open your list in AnyList and tap Add Item.",
                imageDescription: "AnyList's list screen with Add Item highlighted."),
            GroceryListAppGuideStep(
                imageName: "AnyListGuideStep2", text: "Tap the paste list icon at the top right, next to the book.",
                imageDescription: "AnyList's Add Item bar with the paste list icon highlighted, next to the book."),
            GroceryListAppGuideStep(
                imageName: "AnyListGuideStep3", text: "Tap the empty area, then tap Paste.",
                imageDescription: "AnyList's Paste Items screen with the empty text area highlighted."),
            GroceryListAppGuideStep(
                imageName: "AnyListGuideStep4", text: "Each line becomes its own item. Tap the check mark.",
                imageDescription: "The pasted list, one item per line, with the check mark highlighted."),
            GroceryListAppGuideStep(
                imageName: "AnyListGuideStep5", text: "Your items are added and sorted into aisles.",
                imageDescription: "AnyList's list with the items sorted under Bakery and Produce."),
        ],
        // Not documented by AnyList, but it opens the app. If it ever stops, the App Store page
        // still shows Open for someone who has AnyList.
        launchURL: URL(string: "anylist://"),
        appStoreURL: URL(string: "https://apps.apple.com/app/id522167641"))

    /// Every list app offered, in the order the export menu shows them.
    static let all: [GroceryListApp] = [.anyList]
}

/// One numbered step of a list app's paste guide.
nonisolated struct GroceryListAppGuideStep: Equatable, Sendable {
    /// The picture in the asset catalog.
    let imageName: String
    let text: LocalizedStringResource
    /// What the picture shows, for VoiceOver.
    let imageDescription: LocalizedStringResource
}

/// Builds the text a list app is pasted.
///
/// The lines come from `GroceryReminderPlan.drafts`, so exactly the same items are left out as
/// in the Reminders export — checked-off lines and anything the pantry already has — in the
/// same aisle order, with the same amounts in front of the same names.
nonisolated enum GroceryListAppPlan {
    /// The lines still to buy, for `app`.
    static func lines(for drafts: [GroceryReminderDraft], app: GroceryListApp) -> [String] {
        guard !app.sortsIntoItsOwnAisles else { return drafts.map(\.title) }
        var lines: [String] = []
        var aisle: String?
        for draft in drafts {
            if let notes = draft.notes, notes != aisle {
                aisle = notes
                lines.append(notes)
            }
            lines.append(draft.title)
        }
        return lines
    }

    /// What goes on the clipboard: the lines, one per line, and nothing else. No heading and no
    /// trailing newline, because both would paste as items.
    static func text(for drafts: [GroceryReminderDraft], app: GroceryListApp) -> String {
        lines(for: drafts, app: app).joined(separator: "\n")
    }
}
