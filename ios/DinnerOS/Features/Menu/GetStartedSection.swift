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
    var dismissals: any FirstRunDismissalStorage = UserDefaultsFirstRunDismissals()

    @Environment(RecipeLibrary.self) private var library
    @Environment(HouseholdStore.self) private var households
    @Environment(AuthSession.self) private var session

    @State private var isDismissed = false
    @State private var isAddingOwnRecipe = false

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
                    .padding(.horizontal, 16)
            }
        }
        // Whether this member already waved it away, re-read whenever the household changes.
        .onChange(of: DismissalKey(userID: userID, householdID: householdID), initial: true) { _, key in
            guard let userID = key.userID, let householdID = key.householdID else { return }
            isDismissed = dismissals.isDismissed(userID: userID, householdID: householdID)
        }
        .sheet(isPresented: $isAddingOwnRecipe) {
            AddRecipeView()
        }
    }

    private var card: some View {
        VStack(alignment: .leading, spacing: 14) {
            header
            actions
        }
        .padding(16)
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 18))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(Text("Get started"))
    }

    private var header: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            VStack(alignment: .leading, spacing: 4) {
                Text("Let's Fill Your Library")
                    .font(.title3.bold())
                    .accessibilityAddTraits(.isHeader)
                Text("Add recipes to start planning.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 8)
            Button {
                dismiss()
            } label: {
                Image(systemName: "xmark")
                    .font(.footnote.weight(.semibold))
                    .padding(6)
            }
            .buttonStyle(.borderless)
            .accessibilityLabel("Dismiss")
            .accessibilityHint("Hides this until you add recipes.")
        }
    }

    private var actions: some View {
        VStack(alignment: .leading, spacing: 12) {
            NavigationLink(value: DiscoverRoute()) {
                Label(SharedRecipes.title, systemImage: SharedRecipes.systemImage)
            }
            if canAddOwnRecipe {
                Button {
                    isAddingOwnRecipe = true
                } label: {
                    Label("Add a Recipe Yourself", systemImage: "square.and.pencil")
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
            GetStartedSection(dismissals: InMemoryFirstRunDismissals())
                .padding(.horizontal, 16)
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
