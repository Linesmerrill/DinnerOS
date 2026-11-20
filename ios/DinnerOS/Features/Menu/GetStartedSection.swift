import SwiftUI

/// The first thing a household with an empty library sees on the Menu tab: how to get
/// recipes in, by browsing shared recipes or adding one (#530).
///
/// It sits above Your Meals rather than inside All Meals, because the empty state down
/// there is the easiest thing on the screen to scroll past.
///
/// It disappears the moment the household has a recipe, from the catalog or by hand, and
/// can be dismissed before then.
struct GetStartedSection: View {
    /// Where "don't show me this again" is remembered. Injectable so tests needn't touch
    /// the device's own defaults.
    var dismissals: any FirstRunDismissalStorage
    /// Opens the Add a Recipe choices, which the Menu holds. Without one there's no button.
    var onAddOwnRecipe: (() -> Void)?
    /// Told whether the card is showing, so the Menu can leave out its own Shared Recipes row
    /// while this card already offers it (one way in, not three).
    var onShownChange: ((Bool) -> Void)?

    @Environment(RecipeLibrary.self) private var library
    @Environment(HouseholdStore.self) private var households
    @Environment(AuthSession.self) private var session

    @State private var isDismissed = false

    init(
        dismissals: any FirstRunDismissalStorage = UserDefaultsFirstRunDismissals(),
        onAddOwnRecipe: (() -> Void)? = nil,
        onShownChange: ((Bool) -> Void)? = nil
    ) {
        self.dismissals = dismissals
        self.onAddOwnRecipe = onAddOwnRecipe
        self.onShownChange = onShownChange
    }

    private var householdID: String? { households.current?.household.id }
    private var userID: String? { session.currentUser?.id }

    private var canAddOwnRecipe: Bool { households.access?.can(.recipesEdit) == true }

    private var isShown: Bool {
        FirstRunRecipes.shows(
            FirstRunRecipes.Input(
                libraryLoaded: library.phase == .loaded, hasRecipes: !library.items.isEmpty,
                isFiltered: library.filters.isNarrowed, isDismissed: isDismissed))
    }

    var body: some View {
        Group {
            if isShown {
                card
                    .padding(.horizontal, DS.Space.l)
            }
        }
        // Whether this member already waved it away, re-read whenever the household changes.
        .onChange(of: DismissalKey(userID: userID, householdID: householdID), initial: true) { _, key in
            guard let userID = key.userID, let householdID = key.householdID else { return }
            isDismissed = dismissals.isDismissed(userID: userID, householdID: householdID)
        }
        .onChange(of: isShown, initial: true) { _, shown in
            onShownChange?(shown)
        }
    }

    private var card: some View {
        VStack(alignment: .leading, spacing: DS.Space.ml) {
            header
            actions
        }
        .padding(DS.Space.l)
        .background(DS.Colors.surface, in: .rect(cornerRadius: DS.Radius.large))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(Text("Get Started"))
    }

    private var header: some View {
        HStack(alignment: .firstTextBaseline, spacing: DS.Space.s) {
            VStack(alignment: .leading, spacing: DS.Space.xxs) {
                Text("Let’s Fill Your Library")
                    .font(.title3.bold())
                    .accessibilityAddTraits(.isHeader)
                Text("Add recipes to start planning.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: DS.Space.s)
            Button {
                dismiss()
            } label: {
                Image(systemName: "xmark")
                    .font(.footnote.weight(.semibold))
                    .padding(DS.Space.xs)
            }
            .buttonStyle(.borderless)
            .accessibilityLabel("Close")
            .accessibilityHint("Hides this until you add recipes.")
        }
    }

    private var actions: some View {
        VStack(alignment: .leading, spacing: DS.Space.m) {
            NavigationLink(value: DiscoverRoute()) {
                Label(SharedRecipes.title, systemImage: SharedRecipes.systemImage)
            }
            if canAddOwnRecipe, let onAddOwnRecipe {
                Button {
                    onAddOwnRecipe()
                } label: {
                    Label("Add a Recipe", systemImage: "square.and.pencil")
                }
            }
        }
        .font(.subheadline.weight(.medium))
    }

    private func dismiss() {
        isDismissed = true
        guard let userID, let householdID else { return }
        dismissals.dismiss(userID: userID, householdID: householdID)
    }

    /// The member and household a dismissal belongs to.
    private struct DismissalKey: Equatable {
        let userID: String?
        let householdID: String?
    }
}

/// Where the Menu offers Shared Recipes. A new household saw three ways in at once (the Get
/// Started card, the Shared Recipes row, and the empty All Meals), so there is one at a time.
nonisolated enum MenuEntryPoints {
    /// The Shared Recipes row under Your Meals shows only while the Get Started card doesn't.
    static func showsSharedRecipesRow(getStartedShown: Bool) -> Bool {
        !getStartedShown
    }
}

/// The card over an empty library.
private func getStartedPreview(canEdit: Bool = true) -> some View {
    let session = HouseholdPreviewData.session()
    let permissions = Array(HouseholdPermission.allKnown).filter { canEdit || $0 != .recipesEdit }
    let detail = HouseholdDetail(
        household: HouseholdPreviewData.household, members: HouseholdPreviewData.detail.members,
        role: canEdit ? .admin : .member, permissions: permissions)
    let listed = [
        HouseholdListItem(
            household: HouseholdPreviewData.household, role: detail.role, permissions: permissions)
    ]
    return NavigationStack {
        ScrollView {
            GetStartedSection(dismissals: InMemoryFirstRunDismissals(), onAddOwnRecipe: {})
                .padding(.horizontal, DS.Space.l)
        }
    }
    .environment(session)
    .environment(
        HouseholdStore.preview(
            session: session, phase: .ready, current: detail, households: listed)
    )
    .environment(RecipeLibrary.preview(session: session, phase: .loaded, items: []))
}

#Preview("Empty library") {
    getStartedPreview()
}

#Preview("Can't add recipes") {
    getStartedPreview(canEdit: false)
}
