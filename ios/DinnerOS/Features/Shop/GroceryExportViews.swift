import SwiftUI

#if canImport(UIKit)
    import UIKit
#endif

/// Drives "Add to Reminders", Share, and Copy for one week's grocery list.
///
/// A last resort, so it stays quiet: every action reports one short line and nothing blocks
/// the list. The text comes from `GroceryListText`, the same formatter the Grocery List
/// screen's Share button uses, so all three actions produce identical text.
@MainActor
@Observable
final class GroceryExportController {
    /// A list with the week's name is already in Reminders, so the member chooses what to do.
    struct ExistingList: Identifiable, Equatable {
        let name: String
        let drafts: [GroceryReminderDraft]

        var id: String { name }
    }

    /// For example "Added 14 items to Reminders".
    private(set) var message: String?
    private(set) var errorMessage: String?
    private(set) var isWorking = false
    /// The member denied access, so only Settings can undo it.
    var showsAccessDenied = false
    var existingList: ExistingList?

    @ObservationIgnored private let export: GroceryRemindersExport

    /// `nonisolated` so a SwiftUI view can build one in a `@State` initializer.
    nonisolated init(remindersStore: any GroceryRemindersStore) {
        export = GroceryRemindersExport(store: remindersStore)
    }

    /// Sends the unchecked lines to a Reminders list named for the week. Offers Replace or Add
    /// when a list with that name is already there.
    func addToReminders(list: GroceryList, week: ISOWeek, checked: Set<String>, appName: String) async {
        let name = GroceryReminderPlan.listName(appName: appName, week: week)
        let drafts = GroceryReminderPlan.drafts(for: list, checked: checked)
        guard !drafts.isEmpty else {
            report(error: String(localized: "Nothing to add: every item is checked off."))
            return
        }
        isWorking = true
        defer { isWorking = false }
        message = nil
        errorMessage = nil
        do {
            // Access has to come first: the list can't be looked for without it.
            guard try await export.requestAccess() == .granted else {
                showsAccessDenied = true
                return
            }
            if try export.hasExistingList(named: name) {
                existingList = ExistingList(name: name, drafts: drafts)
                return
            }
            let count = try await export.export(drafts, to: name, merge: .add)
            report(added: count)
        } catch {
            handle(error)
        }
    }

    /// Replace or Add, once the member chooses.
    func finishExistingList(_ pending: ExistingList, merge: GroceryRemindersMerge) async {
        existingList = nil
        isWorking = true
        defer { isWorking = false }
        do {
            let count = try await export.export(pending.drafts, to: pending.name, merge: merge)
            report(added: count)
        } catch {
            handle(error)
        }
    }

    /// Puts the same text on the clipboard that Share sends.
    func copy(_ text: String) {
        #if canImport(UIKit)
            UIPasteboard.general.string = text
        #endif
        report(message: String(localized: "Grocery list copied"))
    }

    func dismissMessage() {
        message = nil
        errorMessage = nil
    }

    private func handle(_ error: any Error) {
        switch error {
        case GroceryRemindersError.accessDenied:
            showsAccessDenied = true
        case GroceryRemindersError.nothingToAdd:
            report(error: String(localized: "Nothing to add: every item is checked off."))
        case is CancellationError:
            return
        default:
            report(error: String(localized: "Couldn't add to Reminders. Try again."))
        }
    }

    private func report(added count: Int) {
        report(
            message: count == 1
                ? String(localized: "Added 1 item to Reminders")
                : String(localized: "Added \(count) items to Reminders"))
    }

    private func report(message text: String) {
        errorMessage = nil
        message = text
        AccessibilityNotification.Announcement(text).post()
    }

    private func report(error text: String) {
        message = nil
        errorMessage = text
        AccessibilityNotification.Announcement(text).post()
    }
}

/// The export actions, shared by the Shop tab's section and the Grocery List screen's menu.
struct GroceryExportActions: View {
    let controller: GroceryExportController
    let model: GroceryListModel
    /// `false` where a Share button already sits next to the menu, so it isn't offered twice.
    var includesShare = true

    @Environment(\.appConfiguration) private var configuration

    private var text: String? {
        model.plainText()
    }

    var body: some View {
        Button {
            guard let list = model.list else { return }
            Task {
                await controller.addToReminders(
                    list: list, week: model.week, checked: model.checked, appName: configuration.displayName)
            }
        } label: {
            Label("Add to Reminders", systemImage: "list.bullet.clipboard")
        }
        .disabled(model.list == nil || controller.isWorking)

        if let text {
            if includesShare {
                ShareLink(
                    item: text,
                    subject: Text("Grocery List"),
                    preview: SharePreview(Text("Grocery List: \(model.week.rangeLabel())"))
                ) {
                    Label("Share", systemImage: "square.and.arrow.up")
                }
            }
            Button {
                controller.copy(text)
            } label: {
                Label("Copy", systemImage: "doc.on.doc")
            }
        }
    }
}

/// The Shop tab's quiet fallback: a plain section under the main flow, never a primary button.
struct GroceryExportSection: View {
    let controller: GroceryExportController
    let model: GroceryListModel

    var body: some View {
        Section {
            GroceryExportActions(controller: controller, model: model)
            if let message = controller.message {
                Label(message, systemImage: "checkmark.circle.fill")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .accessibilityHidden(true)
            }
            if let errorMessage = controller.errorMessage {
                FormErrorLabel(message: errorMessage)
                    .font(.footnote)
            }
        } header: {
            Text("Export the List")
        } footer: {
            Text("A fallback when you're not ordering online: take the list to Reminders, Messages, or Notes.")
        }
    }
}

/// The alerts and the Replace-or-Add question, attached wherever the actions are shown.
struct GroceryExportPrompts: ViewModifier {
    let controller: GroceryExportController

    @Environment(\.openURL) private var openURL

    func body(content: Content) -> some View {
        content
            .alert(
                "Reminders Access Is Off",
                isPresented: Binding(
                    get: { controller.showsAccessDenied },
                    set: { isPresented in
                        if !isPresented { controller.showsAccessDenied = false }
                    })
            ) {
                #if canImport(UIKit)
                    Button("Open Settings") {
                        if let url = URL(string: UIApplication.openSettingsURLString) {
                            openURL(url)
                        }
                    }
                #endif
                Button("Not Now", role: .cancel) {}
            } message: {
                Text(
                    "To put your grocery list in Reminders, turn on Reminders for this app in Settings. Share or Copy work without it."
                )
            }
            .confirmationDialog(
                controller.existingList.map { Text("“\($0.name)” already exists") } ?? Text(""),
                isPresented: Binding(
                    presenting: Binding(
                        get: { controller.existingList },
                        set: { controller.existingList = $0 })),
                titleVisibility: .visible,
                presenting: controller.existingList
            ) { pending in
                Button("Replace Its Items", role: .destructive) {
                    Task { await controller.finishExistingList(pending, merge: .replace) }
                }
                Button("Add to It") {
                    Task { await controller.finishExistingList(pending, merge: .add) }
                }
                Button("Cancel", role: .cancel) {
                    controller.existingList = nil
                }
            } message: { pending in
                Text("Replacing removes the \(pending.drafts.count) reminders already in that list.")
            }
    }
}

extension View {
    /// The access and Replace-or-Add prompts for `controller`.
    func groceryExportPrompts(_ controller: GroceryExportController) -> some View {
        modifier(GroceryExportPrompts(controller: controller))
    }
}

#Preview("Export section") {
    let session = HouseholdPreviewData.session()
    Form {
        GroceryExportSection(
            controller: GroceryExportController(remindersStore: PreviewRemindersStore()),
            model: .preview(session: session, list: PlanPreviewData.groceryList, checked: ["garlic"]))
    }
}

/// A Reminders store that does nothing, so previews never touch the real database.
@MainActor
private final class PreviewRemindersStore: GroceryRemindersStore {
    var access: GroceryRemindersAccess { .notDetermined }
    func requestAccess() async throws -> GroceryRemindersAccess { .denied }
    func listID(titled title: String) throws -> String? { nil }
    func makeList(titled title: String) throws -> String { "preview" }
    func clearList(withID id: String) async throws {}
    func addReminders(_ drafts: [GroceryReminderDraft], toListWithID id: String) throws {}
}
