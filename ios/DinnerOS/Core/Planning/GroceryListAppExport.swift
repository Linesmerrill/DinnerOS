import Foundation

/// A grocery app outside DinnerOS that a week's list can be handed to in one action.
///
/// AnyList is the first. None of these apps publishes an API, a URL scheme, or a share
/// extension that takes items, so the handoff every one of them does document is the same:
/// the whole list as plain text, one item per line, pasted in one go. See
/// `docs/shopping-providers.md` for what each vendor actually documents.
///
/// A second app needs one value here and nothing else — the lines, the button, the explainer,
/// and the confirmation all read from it.
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
    /// What to do with the copied text once it's on the clipboard, in the app's own words.
    /// Shown in the explainer before anything is copied, and nowhere else.
    let pasteSteps: LocalizedStringResource

    /// AnyList's documented bulk import: one item per line, pasted into the Add Item field,
    /// categorized by AnyList on arrival.
    /// <https://help.anylist.com/articles/paste-items/>
    static let anyList = GroceryListApp(
        id: "anylist",
        name: "AnyList",
        systemImage: "checklist",
        sortsIntoItsOwnAisles: true,
        pasteSteps: """
            In AnyList, open the list you shop from, tap the Add Item field, tap the paste \
            button, then tap Done. Every line arrives as its own item, and AnyList files each \
            one under its own category.
            """)

    /// Every list app offered, in the order the export menu shows them.
    static let all: [GroceryListApp] = [.anyList]
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
