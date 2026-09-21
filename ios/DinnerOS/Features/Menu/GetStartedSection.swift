import SwiftUI

/// The first thing a household with an empty library sees on the Menu tab: how to get
/// recipes in, with importing a meal-kit order history as the main action (#530).
///
/// It sits above Your Meals rather than inside All Meals, because the empty state down
/// there is the easiest thing on the screen to scroll past, and it starts the *same*
/// sign-in the Household tab uses (`MealKitSignInFlow`) rather than pointing at another
/// tab or keeping a second copy of the flow.
///
/// It disappears the moment the household has a recipe — by import, by the catalog, or by
/// hand — and can be dismissed before then.
struct GetStartedSection: View {
    /// Where "don't show me this again" is remembered. Injectable so tests needn't touch
    /// the device's own defaults.
    var dismissals: any FirstRunDismissalStorage = UserDefaultsFirstRunDismissals()

    @Environment(RecipeLibrary.self) private var library
    @Environment(HouseholdStore.self) private var households
    @Environment(AuthSession.self) private var session
    /// Optional so previews and tests needn't supply one, as on the Household tab.
    @Environment(MealKitImportStore.self) private var mealKit: MealKitImportStore?

    @State private var isDismissed = false
    @State private var isSigningIn = false
    @State private var isAddingOwnRecipe = false
    @State private var actionError: String?

    private var householdID: String? { households.current?.household.id }
    private var userID: String? { session.currentUser?.id }

    private var canImport: Bool { households.access?.can(.recipesImport) == true }
    private var canAddOwnRecipe: Bool { households.access?.can(.recipesEdit) == true }
    private var service: MealKitService { mealKit?.service ?? .helloFresh }

    private var input: FirstRunRecipes.Input {
        FirstRunRecipes.Input(
            libraryLoaded: library.phase == .loaded, hasRecipes: !library.items.isEmpty,
            isFiltered: library.filters.isNarrowed, isDismissed: isDismissed, canImport: canImport,
            importEnabled: mealKit?.isEnabled == true, job: mealKit?.job)
    }

    private var state: FirstRunRecipes.State { FirstRunRecipes.state(for: input) }

    var body: some View {
        Group {
            if state != .hidden {
                card
                    .padding(.horizontal, 16)
            }
        }
        // Whether this member already waved it away, re-read whenever the household changes.
        .onChange(of: DismissalKey(userID: userID, householdID: householdID), initial: true) { _, key in
            guard let userID = key.userID, let householdID = key.householdID else { return }
            isDismissed = dismissals.isDismissed(userID: userID, householdID: householdID)
        }
        // The status is only read for someone who could act on it; without `recipes.import`
        // the request would be a guaranteed 403, exactly as on the Household tab.
        .task(id: StatusKey(householdID: householdID, canRead: shouldReadStatus)) {
            guard let householdID, shouldReadStatus, let mealKit else { return }
            mealKit.activate(householdID: householdID)
            await mealKit.load()
        }
        // Polls only while a run is actually in flight, and stops when it isn't.
        .task(id: mealKit?.isImporting ?? false) {
            await mealKit?.pollWhileImporting()
        }
        // A finished run wrote to the library on the server; read it back so the surface
        // gives way to the recipes instead of claiming they're somewhere the list isn't.
        .onChange(of: mealKit?.job?.state) { _, state in
            guard state == .finished else { return }
            Task { await library.refresh() }
        }
        .sheet(isPresented: $isSigningIn) {
            NavigationStack { MealKitImportFlow() }
        }
        .sheet(isPresented: $isAddingOwnRecipe) {
            AddRecipeView()
        }
    }

    /// Whether this member's status read would be allowed, and worth making.
    private var shouldReadStatus: Bool {
        FirstRunRecipes.readsImportStatus(canImport: canImport, hasRecipes: !library.items.isEmpty)
    }

    private var card: some View {
        VStack(alignment: .leading, spacing: 14) {
            header
            if let message = actionError {
                FormErrorLabel(message: message)
            }
            importBlock
            Divider()
            secondaryActions
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
                Text("There are no recipes in this household yet, so there's nothing to plan with.")
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
            .accessibilityHint("Hides this until you add recipes. You can still import from the Household tab.")
        }
    }

    @ViewBuilder
    private var importBlock: some View {
        switch state {
        case .hidden:
            EmptyView()
        case .offer:
            importOffer(
                title: String(localized: "Import from \(service.displayName)"),
                detail: String(
                    localized:
                        "Sign in on \(service.displayName)'s own page and we'll bring over the recipes you've ordered."
                ),
                button: String(localized: "Import from \(service.displayName)"),
                action: { isSigningIn = true })
        case .importing(let job):
            importing(job)
        case .needsSignIn(let detail):
            importOffer(
                title: String(localized: "Sign in to finish importing"), detail: detail,
                button: String(localized: "Sign In Again"), symbol: "person.badge.key",
                action: { isSigningIn = true })
        case .stopped(let detail):
            importOffer(
                title: String(localized: "The import stopped"), detail: detail,
                button: String(localized: "Try Again"), symbol: "exclamationmark.triangle",
                action: { isSigningIn = true })
        case .foundNothing:
            importOffer(
                title: String(localized: "Nothing to import"),
                detail: String(
                    localized:
                        "We didn't find any recipes on your \(service.displayName) order history. You can try again, or start from the catalog below."
                ),
                button: String(localized: "Try Again"), symbol: "tray",
                action: { isSigningIn = true })
        case .imported(let count):
            imported(count: count)
        case .unavailable(let reason):
            unavailable(reason)
        }
    }

    private func importOffer(
        title: String, detail: String, button: String, symbol: String = "shippingbox.fill",
        action: @escaping () -> Void
    ) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            blurb(title: title, detail: detail, symbol: symbol)
            Button(button, action: action)
                .buttonStyle(.borderedProminent)
                .disabled(mealKit?.isWorking == true)
            Text(MealKitFormatting.credentialExplanation(for: service))
                .font(.caption)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private func importing(_ job: MealKitImportJob) -> some View {
        let summary = MealKitFormatting.summary(for: job, service: service)
        return VStack(alignment: .leading, spacing: 10) {
            blurb(title: summary.title, detail: summary.detail, symbol: summary.symbol)
            if let progress = job.progress {
                ProgressView(value: progress)
            } else {
                ProgressView().progressViewStyle(.linear)
            }
            Text("It keeps going on our server, so you can close the app. We'll let you know when it's done.")
                .font(.caption)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .accessibilityElement(children: .combine)
    }

    private func imported(count: Int) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            blurb(
                title: count == 1
                    ? String(localized: "1 recipe imported") : String(localized: "\(count) recipes imported"),
                detail: String(localized: "They're in your library. This list hasn't caught up yet."),
                symbol: "checkmark.circle")
            Button("Show Them") {
                Task { await library.refresh() }
            }
            .buttonStyle(.borderedProminent)
        }
    }

    private func unavailable(_ reason: FirstRunRecipes.Unavailable) -> some View {
        blurb(
            title: String(localized: "Start from the catalog"),
            detail: {
                switch reason {
                case .noPermission:
                    String(
                        localized:
                            "Importing a meal-kit order history is up to whoever manages this household. You can still browse the catalog or type a recipe."
                    )
                case .notOnThisServer:
                    String(
                        localized:
                            "Meal-kit import isn't available here. You can browse the catalog or type a recipe."
                    )
                }
            }(), symbol: "books.vertical")
    }

    private func blurb(title: String, detail: String, symbol: String) -> some View {
        Label {
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.headline)
                Text(detail)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        } icon: {
            Image(systemName: symbol).foregroundStyle(.tint)
        }
        .labelStyle(.titleAndIcon)
    }

    private var secondaryActions: some View {
        VStack(alignment: .leading, spacing: 12) {
            NavigationLink(value: DiscoverRoute()) {
                Label("Browse the Catalog", systemImage: "sparkles")
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

    /// The household whose import status is being read, and whether it may be read at all.
    private struct StatusKey: Equatable {
        let householdID: String?
        let canRead: Bool
    }
}

/// The surface over an empty library, in the state `status` describes.
private func getStartedPreview(status: MealKitStatus, canImport: Bool = true) -> some View {
    let session = HouseholdPreviewData.session()
    let permissions = Array(HouseholdPermission.allKnown).filter { canImport || $0 != .recipesImport }
    let detail = HouseholdDetail(
        household: HouseholdPreviewData.household, members: HouseholdPreviewData.detail.members,
        role: canImport ? .admin : .member, permissions: permissions)
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
    .environment(MealKitImportStore.preview(session: session, status: status))
}

#Preview("Nothing linked") {
    getStartedPreview(status: MealKitStatus(enabled: true))
}

#Preview("Importing") {
    getStartedPreview(
        status: MealKitStatus(
            enabled: true,
            latestJob: MealKitImportJob(
                id: "job-1", status: "running", phase: "recipes", recipesFound: 48, recipesDone: 12)))
}

#Preview("Found nothing") {
    getStartedPreview(
        status: MealKitStatus(
            enabled: true,
            latestJob: MealKitImportJob(id: "job-1", status: "succeeded", phase: "done")))
}

#Preview("Without permission") {
    getStartedPreview(status: MealKitStatus(enabled: true), canImport: false)
}
