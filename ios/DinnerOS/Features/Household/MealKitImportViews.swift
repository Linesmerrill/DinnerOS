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
                            detail: "Sign in once. We'll fetch the recipes you ordered while you get on with things.",
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
                MealKitSignInForm {
                    isSigningIn = false; onDone()
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

/// The one-time meal-kit sign-in.
///
/// The password lives in this view's state and in the request body, and nowhere else: it is
/// never written to the Keychain or `UserDefaults`, and the field is cleared as soon as the
/// request returns either way.
struct MealKitSignInForm: View {
    var onLinked: () -> Void = {}

    @Environment(MealKitImportStore.self) private var mealKit
    @Environment(\.dismiss) private var dismiss
    @State private var email = ""
    @State private var password = ""
    @State private var errorMessage: String?
    @FocusState private var focus: Field?

    private enum Field { case email, password }

    private var service: MealKitService { mealKit.service }

    private var canSubmit: Bool {
        !email.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && !password.isEmpty && !mealKit.isWorking
    }

    var body: some View {
        Form {
            Section {
                TextField("Email", text: $email)
                    .textContentType(.username)
                    .keyboardType(.emailAddress)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .focused($focus, equals: .email)
                    .submitLabel(.next)
                    .onSubmit { focus = .password }
                SecureField("Password", text: $password)
                    .textContentType(.password)
                    .focused($focus, equals: .password)
                    .submitLabel(.go)
                    .onSubmit { Task { await link() } }
            } header: {
                Text("\(service.displayName) Account")
            } footer: {
                Text(MealKitFormatting.credentialExplanation(for: service))
            }
            if let errorMessage {
                Section {
                    FormErrorLabel(message: errorMessage)
                }
            }
            Section {
                Text("We only read the recipes on your order history — nothing else from your account.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .navigationTitle("Connect \(service.displayName)")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) {
                Button("Cancel") { dismiss() }
            }
            ToolbarItem(placement: .confirmationAction) {
                if mealKit.isWorking {
                    ProgressView()
                } else {
                    Button("Connect") { Task { await link() } }
                        .disabled(!canSubmit)
                }
            }
        }
        .disabled(mealKit.isWorking)
        .onAppear { focus = .email }
    }

    private func link() async {
        guard canSubmit else { return }
        errorMessage = nil
        let submitted = password
        do {
            try await mealKit.link(email: email.trimmingCharacters(in: .whitespacesAndNewlines), password: submitted)
            password = ""
            onLinked()
            dismiss()
        } catch is CancellationError {
            password = ""
        } catch {
            // Never keep the attempt around: a retry types it again.
            password = ""
            errorMessage = HouseholdStore.message(for: error)
            focus = .password
        }
    }
}

/// What was imported, what failed and why, and the way out: sign in again, import again, or
/// unlink.
struct MealKitImportStatusView: View {
    @Environment(MealKitImportStore.self) private var mealKit
    @State private var isSigningIn = false
    @State private var isConfirmingUnlink = false
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
            if mealKit.link != nil {
                actionsSection
            } else if mealKit.isEnabled {
                Section {
                    Button("Connect \(service.displayName)") { isSigningIn = true }
                } footer: {
                    Text(MealKitFormatting.credentialExplanation(for: service))
                }
            }
        }
        .navigationTitle("Recipe Import")
        .navigationBarTitleDisplayMode(.inline)
        .task { await mealKit.load() }
        // Polls only while something is actually in flight, and stops when it isn't.
        .task(id: mealKit.isImporting) { await mealKit.pollWhileImporting() }
        .refreshable { await mealKit.refresh() }
        .sheet(isPresented: $isSigningIn) {
            NavigationStack { MealKitSignInForm() }
        }
        .confirmationDialog(
            "Unlink your \(service.displayName) account?", isPresented: $isConfirmingUnlink, titleVisibility: .visible
        ) {
            Button("Unlink", role: .destructive) { Task { await unlink() } }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text(
                "We'll delete the saved sign-in and stop any import in progress. Recipes already imported stay in your library."
            )
        }
    }

    private var statusSection: some View {
        Section {
            MealKitStatusRow(summary: summary, job: mealKit.job)
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
            if let link = mealKit.link {
                Text(linkFooter(link))
            }
        }
    }

    private func linkFooter(_ link: MealKitLink) -> String {
        let connected = link.linkedAt.formatted(date: .abbreviated, time: .shortened)
        if link.needsSignIn {
            return String(localized: "\(link.accountLabel) connected \(connected). The saved session has expired.")
        }
        return String(localized: "\(link.accountLabel) connected \(connected). Only the session is stored, encrypted.")
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
            Text("These are still on your \(service.displayName) account. Importing again will try them once more.")
        }
    }

    @ViewBuilder
    private var actionsSection: some View {
        Section {
            if summary.needsAttention, mealKit.job?.state == .needsSignIn {
                Button("Sign In Again") { isSigningIn = true }
            } else if !mealKit.isImporting {
                Button("Import Again") { Task { await startImport() } }
                    .disabled(mealKit.isWorking)
            }
            Button("Unlink \(service.displayName)", role: .destructive) { isConfirmingUnlink = true }
                .disabled(mealKit.isWorking)
        } footer: {
            Text("Importing again is safe: recipes you already have are left alone.")
        }
    }

    private func startImport() async {
        actionError = nil
        do {
            try await mealKit.startImport()
        } catch is CancellationError {
        } catch {
            actionError = HouseholdStore.message(for: error)
        }
    }

    private func unlink() async {
        actionError = nil
        do {
            try await mealKit.unlink()
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
        link: MealKitLink(status: "active", accountLabel: "HelloFresh account", linkedAt: .now, updatedAt: .now),
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
        link: MealKitLink(status: "active", accountLabel: "HelloFresh account", linkedAt: .now, updatedAt: .now),
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
