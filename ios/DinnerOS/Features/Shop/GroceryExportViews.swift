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

    /// What "Send to <app>" puts on the clipboard, and the guide to pasting it, shown every
    /// time before anything is copied.
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

    /// For example "Added 14 items to Reminders".
    private(set) var message: String?
    private(set) var errorMessage: String?
    private(set) var isWorking = false
    /// The member denied access, so only Settings can undo it.
    var showsAccessDenied = false
    var existingList: ExistingList?
    /// The first-time explainer, while it's showing.
    var explainer: Explainer?
    /// The guide to pasting into a list app, while it's showing.
    var listAppGuide: ListAppHandoff?

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

    /// Shows the guide to pasting the week's unchecked lines into `app`, every time: testers
    /// tapped Send, saw nothing happen, and pasted the list into one item. Nothing is copied
    /// until the member taps the guide's button.
    ///
    /// With nothing left to buy the guide still opens and says so: an error line alone didn't
    /// show on the Grocery List screen, so the tap looked like it did nothing.
    func send(list: GroceryList, checked: Set<String>, to app: GroceryListApp) {
        listAppGuide = ListAppHandoff(app: app, drafts: GroceryReminderPlan.drafts(for: list, checked: checked))
    }

    /// The guide's Copy button: the lines go on the clipboard, one item per line. The guide
    /// opens the app itself once the member has seen "List copied".
    func copyForListApp(_ handoff: ListAppHandoff) {
        write(handoff.text)
        report(message: String(localized: "List copied"))
    }

    private func write(_ text: String) {
        Clipboard.copy(text)
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
            report(error: String(localized: "Couldn’t add to Reminders. Try again."))
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
                "Not ordering online? Send the list to Reminders, AnyList, Messages, or Notes."
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
            .dsSheet(
                item: Binding(
                    get: { controller.explainer },
                    set: { controller.explainer = $0 })
            ) { pending in
                RemindersExplainerSheet(explainer: pending) {
                    Task { await controller.confirmExplainer(pending) }
                }
            }
            .dsSheet(
                item: Binding(
                    get: { controller.listAppGuide },
                    set: { controller.listAppGuide = $0 })
            ) { pending in
                ListAppGuideSheet(handoff: pending) {
                    controller.copyForListApp(pending)
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
                    .padding(.vertical, DS.Space.hairline)
                }
                Section {
                    ForEach(Array(explainer.preview.enumerated()), id: \.offset) { _, draft in
                        Label {
                            VStack(alignment: .leading, spacing: DS.Space.hairline) {
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
                }
                .buttonStyle(.dsPrimary(fullWidth: true))
                .padding(DS.Space.l)
            }
        }
        .presentationDetents([.medium, .large])
    }
}

/// Shown every time "Send to <app>" is tapped: how to paste the list there, step by step with
/// a picture of each screen, then one button that copies the list and opens the app.
struct ListAppGuideSheet: View {
    let handoff: GroceryExportController.ListAppHandoff
    let copy: () -> Void

    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL
    @State private var copied = false

    private var app: GroceryListApp { handoff.app }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 28) {
                    Text("Your list is pasted in all at once, so every item lands on its own line.")
                        .foregroundStyle(.secondary)
                    if app == .anyList {
                        Label(
                            "Don’t use AnyList Recipe Import from the share sheet. It reads your list as a recipe.",
                            systemImage: "exclamationmark.triangle"
                        )
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    }
                    ForEach(Array(app.guide.enumerated()), id: \.offset) { index, step in
                        ListAppGuideStepView(number: index + 1, step: step)
                    }
                }
                .padding(.horizontal, DS.Space.xl)
                .padding(.vertical, DS.Space.l)
                .frame(maxWidth: 560)
                .frame(maxWidth: .infinity)
            }
            .navigationTitle("Add Your List to \(app.name)")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .safeAreaInset(edge: .bottom) {
                VStack(spacing: DS.Space.s) {
                    if handoff.drafts.isEmpty {
                        Text("Nothing to copy yet. Add recipes to this week, or uncheck items you still need.")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .multilineTextAlignment(.center)
                    }
                    Button {
                        copyAndOpen()
                    } label: {
                        Text("Copy List and Open \(app.name)")
                    }
                    .buttonStyle(.dsPrimary(fullWidth: true))
                    .disabled(copied || handoff.drafts.isEmpty)
                }
                .padding(DS.Space.l)
                .dsReadableWidth()
                .background(DS.Colors.barBackground)
                .overlay(alignment: .top) { Divider() }
            }
        }
    }

    private func copyAndOpen() {
        copy()
        copied = true
        Task {
            // Long enough to see the Copied HUD before the other app takes over.
            try? await Task.sleep(for: CopiedHUD.duration)
            open()
            copied = false
        }
    }

    private func open() {
        #if canImport(UIKit)
            if let launch = app.launchURL, UIApplication.shared.canOpenURL(launch) {
                openURL(launch)
                return
            }
        #endif
        if let store = app.appStoreURL { openURL(store) }
    }
}

/// One numbered step: the number, what to do, and the picture of that screen.
private struct ListAppGuideStepView: View {
    let number: Int
    let step: GroceryListAppGuideStep

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Space.m) {
            HStack(alignment: .firstTextBaseline, spacing: DS.Space.m) {
                Text("\(number)")
                    .font(.headline)
                    .foregroundStyle(DS.Colors.onAccent)
                    .frame(minWidth: DS.Size.iconColumn, minHeight: DS.Size.iconColumn)
                    .background(Circle().fill(DS.Colors.accent))
                    .accessibilityHidden(true)
                Text(step.text)
                    .font(.body.weight(.medium))
                    .fixedSize(horizontal: false, vertical: true)
            }
            .accessibilityElement(children: .combine)
            .accessibilityLabel(Text("Step \(number). ") + Text(step.text))
            Image(step.imageName)
                .resizable()
                .scaledToFit()
                .frame(maxWidth: .infinity)
                .clipShape(.rect(cornerRadius: DS.Radius.medium))
                .overlay(RoundedRectangle(cornerRadius: DS.Radius.medium).strokeBorder(Color(.separator)))
                .accessibilityLabel(Text(step.imageDescription))
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
    .dsReadableForm()
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
