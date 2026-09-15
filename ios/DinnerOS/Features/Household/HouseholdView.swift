import SwiftUI

/// The Household tab. For now: the signed-in account and sign-out. Household
/// membership and invitations arrive in Phase 3.
struct HouseholdView: View {
    @Environment(AuthSession.self) private var session
    @Environment(\.appConfiguration) private var configuration

    @State private var me: MeResponse?
    @State private var isLoading = false
    @State private var loadError: String?
    @State private var isConfirmingSignOut = false

    var body: some View {
        List {
            accountSection
            householdSection
            Section {
                Button("Sign Out", role: .destructive) {
                    isConfirmingSignOut = true
                }
                .accessibilityHint("Signs you out of \(configuration.displayName) on this device.")
            }
        }
        .navigationTitle("Household")
        .task { await load() }
        .refreshable { await load() }
        .confirmationDialog(
            "Sign out of \(configuration.displayName)?",
            isPresented: $isConfirmingSignOut,
            titleVisibility: .visible
        ) {
            Button("Sign Out", role: .destructive) {
                Task { await session.signOut() }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("You'll need to sign in again to see your household.")
        }
    }

    @ViewBuilder
    private var accountSection: some View {
        let user = me?.user ?? session.currentUser
        Section("Account") {
            if let user {
                LabeledContent("Name") {
                    Text(user.displayName.isEmpty ? String(localized: "Not provided") : user.displayName)
                }
                LabeledContent("Email") {
                    Text(user.primaryEmail ?? String(localized: "Not shared"))
                }
            }
            if let me, !me.identities.isEmpty {
                LabeledContent("Signed in with") {
                    Text(me.identities.map { Self.providerName($0.provider) }.formatted(.list(type: .and)))
                }
            }
            if isLoading && me == nil {
                HStack {
                    ProgressView()
                    Text("Loading account…")
                        .foregroundStyle(.secondary)
                }
                .accessibilityElement(children: .combine)
            }
            if let loadError {
                VStack(alignment: .leading, spacing: 8) {
                    Label(loadError, systemImage: "exclamationmark.triangle.fill")
                        .symbolRenderingMode(.multicolor)
                    Button("Try Again") {
                        Task { await load() }
                    }
                }
            }
        }
    }

    private var householdSection: some View {
        Section("Household") {
            VStack(alignment: .leading, spacing: 6) {
                Label("Cook together", systemImage: "person.2.fill")
                    .font(.headline)
                    .foregroundStyle(.tint)
                Text("Soon you'll be able to create a household and invite the people you plan dinners with.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            .padding(.vertical, 4)
            .accessibilityElement(children: .combine)
        }
    }

    private func load() async {
        guard !isLoading else { return }
        isLoading = true
        defer { isLoading = false }
        do {
            me = try await session.loadCurrentUser()
            loadError = nil
        } catch is CancellationError {
            // The view went away; nothing to show.
        } catch {
            // After a sign-out the root view replaces this screen, so no error is shown.
            guard session.currentUser != nil else { return }
            loadError =
                (error as? LocalizedError)?.errorDescription
                ?? String(localized: "Couldn't load your account.")
        }
    }

    private static func providerName(_ provider: String) -> String {
        switch provider {
        case "apple": "Apple"
        case "google": "Google"
        case "dev": String(localized: "Developer")
        default: provider
        }
    }
}

#Preview {
    NavigationStack {
        HouseholdView()
    }
    .environment(
        AuthSession.preview(
            .signedIn(
                UserSummary(
                    id: "preview", displayName: "Ada Lovelace", primaryEmail: "ada@example.com", createdAt: .now))))
}
