import SwiftUI

/// The four ways to add a recipe, in the order they're shown (decision 709).
enum AddRecipeChoice: String, CaseIterable, Identifiable, Hashable {
    case write
    case importIt = "import"
    case shared
    case companion

    var id: String { rawValue }

    var title: String {
        switch self {
        case .write: String(localized: "Write Your Own")
        case .importIt: String(localized: "Import From a Website")
        case .shared: String(localized: "Browse Shared Recipes")
        case .companion: String(localized: "Add a Companion")
        }
    }

    var subtitle: String {
        switch self {
        case .write: String(localized: "Type in the name, ingredients and steps.")
        case .importIt: String(localized: "Paste a link to a recipe, or paste its text.")
        case .shared: String(localized: "Pick from recipes other households share.")
        case .companion: String(localized: "Something that goes with dinner, like bread or a salad.")
        }
    }

    var systemImage: String {
        switch self {
        case .write: "square.and.pencil"
        case .importIt: "link"
        case .shared: SharedRecipes.systemImage
        case .companion: "takeoutbag.and.cup.and.straw"
        }
    }

    /// `AddChoice-write`, `AddChoice-import`, `AddChoice-shared`, `AddChoice-companion`.
    var accessibilityID: String { "AddChoice-\(rawValue)" }
}

/// "Add a Recipe": pick how, then that screen opens in the same sheet. Browse Shared Recipes
/// closes the sheet and the caller opens Shared Recipes.
struct AddRecipeChooser: View {
    /// A recipe was saved; the sheet is closing.
    var onSaved: (Recipe) -> Void = { _ in }
    /// Browse Shared Recipes was picked; the caller closes the sheet and opens it.
    let onBrowseShared: () -> Void

    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss

    @State private var path: [AddRecipeChoice] = []
    /// The companion editor has its own navigation bar, so it opens as a sheet, not a push.
    @State private var addingCompanion = false

    var body: some View {
        NavigationStack(path: $path) {
            List {
                Section {
                    ForEach(AddRecipeChoice.allCases) { choice in
                        Button {
                            pick(choice)
                        } label: {
                            row(choice)
                        }
                        .buttonStyle(.plain)
                        .accessibilityIdentifier(choice.accessibilityID)
                    }
                }
            }
            .navigationTitle("Add a Recipe")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
            .navigationDestination(for: AddRecipeChoice.self) { choice in
                destination(choice)
            }
        }
        .sheet(isPresented: $addingCompanion) {
            if let householdID = households.current?.household.id {
                CompanionEditorView(
                    householdID: householdID,
                    onSaved: {
                        addingCompanion = false
                        dismiss()
                    })
            }
        }
        .accessibilityIdentifier("AddRecipeChooser")
    }

    private func row(_ choice: AddRecipeChoice) -> some View {
        HStack(spacing: DS.Space.ml) {
            Image(systemName: choice.systemImage)
                .font(.title3)
                .foregroundStyle(.tint)
                .frame(width: 32)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: DS.Space.hairline) {
                Text(choice.title)
                    .font(.body.weight(.semibold))
                Text(choice.subtitle)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 8)
            Image(systemName: "chevron.forward")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(.tertiary)
                .accessibilityHidden(true)
        }
        .padding(.vertical, DS.Space.xs)
        .contentShape(.rect)
    }

    private func pick(_ choice: AddRecipeChoice) {
        if choice == .shared {
            onBrowseShared()
        } else if choice == .companion {
            addingCompanion = true
        } else {
            path.append(choice)
        }
    }

    @ViewBuilder
    private func destination(_ choice: AddRecipeChoice) -> some View {
        switch choice {
        case .write:
            AddRecipeForm(start: .write, title: "Write Your Own", onSaved: onSaved, fromChooser: true) { dismiss() }
        case .importIt:
            AddRecipeForm(start: .importIt, title: "Import From a Website", onSaved: onSaved, fromChooser: true) {
                dismiss()
            }
        case .companion, .shared:
            EmptyView()
        }
    }
}

/// The Add a Recipe sheet: the choices, or a recipe link shared from Safari, which waits in
/// the keychain until the app comes forward and then opens straight on reading it.
struct AddRecipeChooserSheet: ViewModifier {
    @Binding var isPresented: Bool
    let canAdd: Bool
    let onSaved: (Recipe) -> Void
    let onBrowseShared: () -> Void

    @State private var sharedLink: String?
    /// The recipe just saved, opened once the sheet has finished closing.
    @State private var saved: Recipe?
    /// Browse Shared Recipes, opened once the sheet has finished closing.
    @State private var browse = false
    @Environment(\.scenePhase) private var phase

    func body(content: Content) -> some View {
        content
            .sheet(
                isPresented: $isPresented,
                onDismiss: {
                    if browse {
                        browse = false
                        onBrowseShared()
                    }
                    guard let recipe = saved else { return }
                    saved = nil
                    onSaved(recipe)
                }
            ) {
                if let sharedLink {
                    AddRecipeView(initialLink: sharedLink, onSaved: { saved = $0 })
                } else {
                    AddRecipeChooser(
                        onSaved: { saved = $0 },
                        onBrowseShared: {
                            browse = true
                            isPresented = false
                        })
                }
            }
            .onChange(of: isPresented) { _, isOpen in
                if !isOpen { sharedLink = nil }
            }
            .onChange(of: phase, initial: true) { _, phase in
                guard phase == .active, canAdd, !isPresented, let link = SharedRecipeLink.take() else { return }
                sharedLink = link
                isPresented = true
            }
    }
}
