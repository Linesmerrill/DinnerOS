import SwiftUI

#if canImport(UIKit)
    import UIKit
#endif

/// Drives "Add to Reminders", "Send to AnyList", Share, and Copy for one week's grocery list.
///
/// A last resort, so it stays quiet: every action reports one short line and nothing blocks
/// the list. Share and Copy use `GroceryListText`, the same formatter the Grocery List screen's
/// Share button uses, so all three produce identical text. Reminders and the list apps use
/// `GroceryReminderPlan.drafts`, so they leave out the same lines in the same order.
@MainActor
@Observable
final class GroceryExportController {
    /// A list with the week's name is already in Reminders, so the member chooses what to do.
    struct ExistingList: Identifiable, Equatable {
        let name: String
        let drafts: [GroceryReminderDraft]

        var id: String { name }
    }

    /// What "Add to Reminders" is about to do, shown once per device before anything is written
    /// or Reminders access is asked for.
    struct Explainer: Identifiable, Equatable {
        /// The exact list name that will be created.
        let name: String
        let drafts: [GroceryReminderDraft]

        var id: String { name }

        /// The first few reminders, as they'll appear.
        var preview: [GroceryReminderDraft] { Array(drafts.prefix(Self.previewCount)) }
        /// How many aren't in the preview.
        var moreCount: Int { max(drafts.count - Self.previewCount, 0) }

        static let previewCount = 4
    }

    /// What "Send to <app>" is about to put on the clipboard, shown once per device per app
    /// before anything is copied.
    struct ListAppHandoff: Identifiable, Equatable {
        let app: GroceryListApp
        /// The lines still to buy, the same drafts the Reminders export would write.
        let drafts: [GroceryReminderDraft]

        var id: String { app.id }

        /// Exactly what goes on the clipboard.
        var text: String { GroceryListAppPlan.text(for: drafts, app: app) }
        /// The first few items, as they'll paste.
        var preview: [String] { Array(GroceryListAppPlan.lines(for: drafts, app: app).prefix(Self.previewCount)) }
        /// How many items aren't in the preview.
        var moreCount: Int { max(drafts.count - Self.previewCount, 0) }

        static let previewCount = 4
    }

    /// The `UserDefaults` key set once Reminders export has worked on this device.
    static let explainedKey = "groceryExport.remindersExplained"

    /// The `UserDefaults` key set once `app`'s export has worked on this device. Per app, so
    /// adding a second list app explains itself the first time rather than riding on AnyList's.
    static func explainedKey(for app: GroceryListApp) -> String {
        "groceryExport.listAppExplained.\(app.id)"
    }

    /// For example "Added 14 items to Reminders".
    private(set) var message: String?
    private(set) var errorMessage: String?
    private(set) var isWorking = false
    /// The member denied access, so only Settings can undo it.
    var showsAccessDenied = false
    var existingList: ExistingList?
    /// The first-time explainer, while it's showing.
    var explainer: Explainer?
    /// The first-time explainer for a list app, while it's showing.
    var listAppExplainer: ListAppHandoff?

    @ObservationIgnored private let export: GroceryRemindersExport
    /// The `UserDefaults` suite, `nil` for standard. A name rather than the object, which isn't
    /// `Sendable`, so the initializer can stay `nonisolated`.
    @ObservationIgnored private let defaultsSuite: String?

    private var defaults: UserDefaults {
        defaultsSuite.flatMap(UserDefaults.init(suiteName:)) ?? .standard
    }

    /// `nonisolated` so a SwiftUI view can build one in a `@State` initializer.
    nonisolated init(remindersStore: any GroceryRemindersStore, defaultsSuite: String? = nil) {
        export = GroceryRemindersExport(store: remindersStore)
        self.defaultsSuite = defaultsSuite
    }

    /// Whether Reminders export has already worked on this device, so the explainer is skipped.
    var hasExplained: Bool {
        defaults.bool(forKey: Self.explainedKey)
    }

    /// Whether `app`'s export has already worked on this device.
    func hasExplained(_ app: GroceryListApp) -> Bool {
        defaults.bool(forKey: Self.explainedKey(for: app))
    }

    /// Sends the unchecked lines to a Reminders list named for the week. The first time on a
    /// device it explains what will happen first, before Reminders access is asked for. Offers
    /// Replace or Add when a list with that name is already there.
    func addToReminders(
        list: GroceryList, week: ISOWeek, weekStartsOn: PlanDay, checked: Set<String>, appName: String
    ) async {
        let name = GroceryReminderPlan.listName(appName: appName, week: week, weekStartsOn: weekStartsOn)
        let drafts = GroceryReminderPlan.drafts(for: list, checked: checked)
        guard !drafts.isEmpty else {
            report(error: String(localized: "Nothing to add: every item is checked off."))
            return
        }
        guard hasExplained else {
            explainer = Explainer(name: name, drafts: drafts)
            return
        }
        await send(drafts, to: name)
    }

    /// The member tapped Add to Reminders in the explainer.
    func confirmExplainer(_ pending: Explainer) async {
        explainer = nil
        await send(pending.drafts, to: pending.name)
    }

    private func send(_ drafts: [GroceryReminderDraft], to name: String) async {
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
        write(text)
        report(message: String(localized: "Grocery list copied"))
    }

    /// Copies the week's unchecked lines in the shape `app` imports them. The first time on a
    /// device it explains what will happen and where to paste, before the clipboard is touched.
    ///
    /// No app is launched: none of them documents a URL scheme that takes items, and opening
    /// one on a guess would leave the member somewhere they didn't ask to be with no way to
    /// tell whether the copy worked.
    func send(list: GroceryList, checked: Set<String>, to app: GroceryListApp) {
        let drafts = GroceryReminderPlan.drafts(for: list, checked: checked)
        guard !drafts.isEmpty else {
            report(error: String(localized: "Nothing to send: every item is checked off."))
            return
        }
        let handoff = ListAppHandoff(app: app, drafts: drafts)
        guard hasExplained(app) else {
            listAppExplainer = handoff
            return
        }
        finish(handoff)
    }

    /// The member tapped Copy Items in the list app's explainer.
    func confirmListAppExplainer(_ pending: ListAppHandoff) {
        listAppExplainer = nil
        finish(pending)
    }

    private func finish(_ handoff: ListAppHandoff) {
        write(handoff.text)
        defaults.set(true, forKey: Self.explainedKey(for: handoff.app))
        let count = handoff.drafts.count
        report(
            message: count == 1
                ? String(localized: "Copied 1 item for \(handoff.app.name)")
                : String(localized: "Copied \(count) items for \(handoff.app.name)"))
    }

    private func write(_ text: String) {
        #if canImport(UIKit)
            UIPasteboard.general.string = text
        #endif
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
        defaults.set(true, forKey: Self.explainedKey)
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
                    list: list, week: model.week, weekStartsOn: model.weekStartsOn, checked: model.checked,
                    appName: configuration.displayName)
            }
        } label: {
            Label("Add to Reminders", systemImage: "list.bullet.clipboard")
        }
        .disabled(model.list == nil || controller.isWorking)

        ForEach(GroceryListApp.all) { app in
            Button {
                guard let list = model.list else { return }
                controller.send(list: list, checked: model.checked, to: app)
            } label: {
                Label("Send to \(app.name)", systemImage: app.systemImage)
            }
            .disabled(model.list == nil || controller.isWorking)
        }

        if let text {
            if includesShare {
                ShareLink(
                    item: text,
                    subject: Text("Grocery List"),
                    preview: SharePreview(
                        Text("Grocery List: \(model.week.rangeLabel(weekStartsOn: model.weekStartsOn))"))
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
            Text(
                "A fallback when you're not ordering online: take the list to Reminders, AnyList, Messages, or Notes."
            )
        }
    }
}

/// The alerts and the Replace-or-Add question, attached wherever the actions are shown.
struct GroceryExportPrompts: ViewModifier {
    let controller: GroceryExportController

    @Environment(\.openURL) private var openURL

    func body(content: Content) -> some View {
        content
            .sheet(
                item: Binding(
                    get: { controller.explainer },
                    set: { controller.explainer = $0 })
            ) { pending in
                RemindersExplainerSheet(explainer: pending) {
                    Task { await controller.confirmExplainer(pending) }
                }
            }
            .sheet(
                item: Binding(
                    get: { controller.listAppExplainer },
                    set: { controller.listAppExplainer = $0 })
            ) { pending in
                ListAppExplainerSheet(handoff: pending) {
                    controller.confirmListAppExplainer(pending)
                }
            }
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

/// Shown the first time "Add to Reminders" is tapped on a device: the list it creates, what
/// each reminder is, and the first few as they'll look.
private struct RemindersExplainerSheet: View {
    let explainer: GroceryExportController.Explainer
    let confirm: () -> Void

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text(
                        "Creates a new list in Reminders called “\(explainer.name)”. Each grocery item becomes a reminder you can tick off while you shop."
                    )
                    .padding(.vertical, 2)
                }
                Section {
                    ForEach(Array(explainer.preview.enumerated()), id: \.offset) { _, draft in
                        Label {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(draft.title)
                                if let notes = draft.notes {
                                    Text(notes)
                                        .font(.footnote)
                                        .foregroundStyle(.secondary)
                                }
                            }
                        } icon: {
                            Image(systemName: "circle")
                                .foregroundStyle(.secondary)
                        }
                        .accessibilityElement(children: .combine)
                    }
                    if explainer.moreCount > 0 {
                        Text("and \(explainer.moreCount) more")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                } header: {
                    Text("Preview")
                }
            }
            .navigationTitle("Add to Reminders")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
            .safeAreaInset(edge: .bottom) {
                Button {
                    dismiss()
                    confirm()
                } label: {
                    Text("Add to Reminders")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                .padding()
            }
        }
        .presentationDetents([.medium, .large])
    }
}

/// Shown the first time "Send to <app>" is tapped on a device: what lands on the clipboard,
/// where to paste it, and the first few lines as they'll arrive.
///
/// It runs before the copy, not after, so the member's clipboard isn't taken over by something
/// they hadn't agreed to yet.
private struct ListAppExplainerSheet: View {
    let handoff: GroceryExportController.ListAppHandoff
    let confirm: () -> Void

    @Environment(\.dismiss) private var dismiss

    private var count: Int { handoff.drafts.count }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text(
                        "Copies the \(count) items you still need, one per line with their amounts. Nothing leaves your phone, and \(handoff.app.name) isn't opened for you."
                    )
                    .padding(.vertical, 2)
                    Text(handoff.app.pasteSteps)
                        .padding(.vertical, 2)
                }
                Section {
                    ForEach(Array(handoff.preview.enumerated()), id: \.offset) { _, line in
                        Text(line)
                    }
                    if handoff.moreCount > 0 {
                        Text("and \(handoff.moreCount) more")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                } header: {
                    Text("Preview")
                }
            }
            .navigationTitle("Send to \(handoff.app.name)")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
            .safeAreaInset(edge: .bottom) {
                Button {
                    dismiss()
                    confirm()
                } label: {
                    Text("Copy Items")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                .padding()
            }
        }
        .presentationDetents([.medium, .large])
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
