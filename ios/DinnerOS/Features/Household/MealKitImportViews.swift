import SwiftUI

/// Offered right after a household is created: bring the recipes you already ordered, or
/// skip and add them by hand later (`docs/meal-kit-import.md`).
///
/// Skipping is a real choice, not a dead end — it is a plain button, not a dismissal, and
/// the offer stays available from the Household tab afterwards.
struct MealKitImportOfferView: View {
    /// Called when the member is done here, whichever way they chose.
    var onDone: () -> Void = {}

    @Environment(MealKitImportStore.self) private var mealKit
    @ScaledMetric(relativeTo: .largeTitle) private var markSize: CGFloat = 56
    @State private var isSigningIn = false

    private var service: MealKitService { mealKit.service }

    var body: some View {
        List {
            Section {
                header
                    .listRowBackground(Color.clear)
            }
            if mealKit.isEnabled {
                Section {
                    Button {
                        isSigningIn = true
                    } label: {
                        MealKitOptionLabel(
                            title: "Import from \(service.displayName)",
                            detail:
                                "Sign in on \(service.displayName)'s own page. We'll fetch what you ordered.",
                            systemImage: "shippingbox.fill")
                    }
                } footer: {
                    Text(MealKitFormatting.credentialExplanation(for: service))
                }
            }
            Section {
                Button {
                    onDone()
                } label: {
                    MealKitOptionLabel(
                        title: "Add Recipes Myself",
                        detail: mealKit.isEnabled
                            ? "Skip the import. You can start it later from the Household tab."
                            : "Recipe import isn't available on this server yet.",
                        systemImage: "square.and.pencil")
                }
            }
        }
        .navigationTitle("Bring Your Recipes")
        .navigationBarTitleDisplayMode(.inline)
        .task { await mealKit.load() }
        .sheet(isPresented: $isSigningIn) {
            NavigationStack {
                MealKitImportFlow {
                    isSigningIn = false
                    onDone()
                }
            }
        }
    }

    private var header: some View {
        VStack(spacing: 12) {
            Image(systemName: "takeoutbag.and.cup.and.straw.fill")
                .font(.system(size: markSize))
                .foregroundStyle(.tint)
                .accessibilityHidden(true)
            Text("Start With What You've Cooked")
                .font(.title2.bold())
                .accessibilityAddTraits(.isHeader)
            Text(
                "If you've had a meal kit, we can bring those recipes in so your library isn't empty on day one."
            )
            .font(.subheadline)
            .foregroundStyle(.secondary)
        }
        .multilineTextAlignment(.center)
        .frame(maxWidth: .infinity)
        .padding(.vertical, 8)
    }
}

/// The meal-kit import: the service's own login page, then the order history read inside that
/// session, then the run queued on our own API.
///
/// There is no password field here, and there never was one that worked. There is no stored
/// session either: the sign-in lives and dies inside `MealKitWebLoginView`, and what comes back
/// is a list of recipes the household was delivered.
struct MealKitImportFlow: View {
    var onQueued: () -> Void = {}

    @Environment(MealKitImportStore.self) private var mealKit
    @Environment(\.dismiss) private var dismiss
    /// Kept only while the queueing call is failing, so **Try Again** does not mean signing in
    /// again. It holds no credential — just recipe ids, page URLs and weeks.
    @State private var harvest: MealKitHarvest?
    @State private var errorMessage: String?

    private var service: MealKitService { mealKit.service }

    var body: some View {
        MealKitWebLoginView(
            service: service,
            // Where the server's cursor says this household's orders have been read to, so a
            // history too long for one sitting resumes instead of starting at today again.
            history: mealKit.history,
            onHarvest: { harvest in
                self.harvest = harvest
                Task { await queue(harvest) }
            },
            onCancel: { dismiss() }
        )
        .alert("Couldn't start the import", isPresented: showingError) {
            Button("Try Again") {
                guard let harvest else { return }
                Task { await queue(harvest) }
            }
            Button("Not Now", role: .cancel) { dismiss() }
        } message: {
            Text(errorMessage ?? "")
        }
    }

    private var showingError: Binding<Bool> {
        Binding(get: { errorMessage != nil }, set: { if !$0 { errorMessage = nil } })
    }

    private func queue(_ harvest: MealKitHarvest) async {
        errorMessage = nil
        do {
            try await mealKit.startImport(harvest: harvest)
            self.harvest = nil
            onQueued()
            dismiss()
        } catch is CancellationError {
        } catch {
            errorMessage = HouseholdStore.message(for: error)
        }
    }
}

/// What was imported, what failed and why, and the way out: import again, or stop the one that
/// is running.
struct MealKitImportStatusView: View {
    @Environment(MealKitImportStore.self) private var mealKit
    @State private var isSigningIn = false
    @State private var isConfirmingStop = false
    @State private var actionError: String?

    private var service: MealKitService { mealKit.service }
    private var summary: MealKitFormatting.Summary {
        MealKitFormatting.summary(for: mealKit.job, service: service)
    }

    var body: some View {
        List {
            if let refreshError = mealKit.refreshError {
                Section { FormErrorLabel(message: refreshError) }
            }
            if let actionError {
                Section { FormErrorLabel(message: actionError) }
            }
            statusSection
            if let job = mealKit.job, !job.failures.isEmpty {
                failuresSection(job)
            }
            if mealKit.isEnabled {
                actionsSection
            }
        }
        .navigationTitle("Recipe Import")
        .navigationBarTitleDisplayMode(.inline)
        .task { await mealKit.load() }
        // Polls only while something is actually in flight, and stops when it isn't.
        .task(id: mealKit.isImporting) { await mealKit.pollWhileImporting() }
        .refreshable { await mealKit.refresh() }
        .sheet(isPresented: $isSigningIn) {
            NavigationStack { MealKitImportFlow() }
        }
        .confirmationDialog(
            "Stop this import?", isPresented: $isConfirmingStop, titleVisibility: .visible
        ) {
            Button("Stop Importing", role: .destructive) { Task { await stop() } }
            Button("Keep Going", role: .cancel) {}
        } message: {
            Text("Recipes already imported stay in your library. You can import again whenever you like.")
        }
    }

    /// What to say about history that hasn't been read yet, when there is any.
    private var moreHistoryNote: String? {
        MealKitFormatting.moreHistoryNote(
            for: mealKit.job, history: mealKit.history, service: service)
    }

    private var statusSection: some View {
        Section {
            MealKitStatusRow(summary: summary, job: mealKit.job)
            if let moreHistoryNote {
                Label(moreHistoryNote, systemImage: "clock.arrow.circlepath")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .padding(.vertical, 2)
            }
            if let job = mealKit.job, job.reviewItems > 0 {
                NavigationLink {
                    ImportReviewView()
                } label: {
                    Label("Import Review", systemImage: "tray.full")
                        .badge(job.reviewItems)
                }
            }
        } header: {
            Text("\(service.displayName)")
        } footer: {
            Text(
                "Nothing about your \(service.displayName) account is saved — not your password, not your sign-in. "
                    + "Importing again reads your orders afresh."
            )
        }
    }

    private func failuresSection(_ job: MealKitImportJob) -> some View {
        Section {
            ForEach(job.failures) { failure in
                VStack(alignment: .leading, spacing: 2) {
                    Text(failure.title)
                        .font(.body)
                    Text(failure.reason)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                .padding(.vertical, 2)
            }
        } header: {
            Text("Couldn't Import")
        } footer: {
            Text("These are still on your \(service.displayName) orders. Importing again will try them once more.")
        }
    }

    @ViewBuilder
    private var actionsSection: some View {
        Section {
            if mealKit.isImporting {
                Button("Stop Importing", role: .destructive) { isConfirmingStop = true }
                    .disabled(mealKit.isWorking)
            } else {
                Button(importButtonTitle) {
                    isSigningIn = true
                }
                .disabled(mealKit.isWorking)
            }
        } footer: {
            Text(MealKitFormatting.credentialExplanation(for: service))
        }
    }

    /// "Import Again" is the wrong promise when the last run left history behind: that run is
    /// going to be *continued*, not repeated.
    private var importButtonTitle: LocalizedStringKey {
        if moreHistoryNote != nil { return "Fetch More History" }
        return mealKit.job == nil ? "Import from \(service.displayName)" : "Import Again"
    }

    private func stop() async {
        actionError = nil
        do {
            try await mealKit.stopImports()
        } catch is CancellationError {
        } catch {
            actionError = HouseholdStore.message(for: error)
        }
    }
}

/// One status line: what is happening, and how far along when that is known.
struct MealKitStatusRow: View {
    let summary: MealKitFormatting.Summary
    let job: MealKitImportJob?

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Label {
                Text(summary.title).font(.headline)
            } icon: {
                Image(systemName: summary.symbol)
                    .foregroundStyle(summary.needsAttention ? AnyShapeStyle(.orange) : AnyShapeStyle(.tint))
            }
            Text(summary.detail)
                .font(.subheadline)
                .foregroundStyle(.secondary)
            if let job, job.isWorking {
                if let progress = job.progress {
                    ProgressView(value: progress)
                } else {
                    ProgressView().progressViewStyle(.linear)
                }
            }
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .combine)
    }
}

/// The shared label for the onboarding choices.
private struct MealKitOptionLabel: View {
    let title: LocalizedStringKey
    let detail: LocalizedStringKey
    let systemImage: String

    var body: some View {
        Label {
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.headline)
                Text(detail)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        } icon: {
            Image(systemName: systemImage).foregroundStyle(.tint)
        }
        .padding(.vertical, 6)
    }
}

#Preview("Offer") {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        MealKitImportOfferView()
    }
    .environment(session)
    .environment(MealKitImportStore.preview(session: session, status: MealKitStatus(enabled: true)))
}

#Preview("Importing") {
    let session = HouseholdPreviewData.session()
    let status = MealKitStatus(
        enabled: true,
        latestJob: MealKitImportJob(
            id: "job-1", status: "running", phase: "recipes", recipesFound: 48, recipesDone: 12))
    NavigationStack {
        MealKitImportStatusView()
    }
    .environment(session)
    .environment(MealKitImportStore.preview(session: session, status: status))
}

#Preview("Finished with failures") {
    let session = HouseholdPreviewData.session()
    let status = MealKitStatus(
        enabled: true,
        latestJob: MealKitImportJob(
            id: "job-1", status: "succeeded", phase: "done", recipesFound: 48, recipesDone: 47,
            imported: 45, updated: 2, unchanged: 1, reviewItems: 6,
            failures: [
                MealKitImportFailure(
                    sourceRecipeID: "6512aa11bb22cc33dd44ee55", name: "Sheet Pan Chicken",
                    reason: "the page carried no recipe")
            ]))
    NavigationStack {
        MealKitImportStatusView()
    }
    .environment(session)
    .environment(MealKitImportStore.preview(session: session, status: status))
}
