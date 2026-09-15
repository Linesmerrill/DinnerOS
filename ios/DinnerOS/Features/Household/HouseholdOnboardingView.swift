import SwiftUI

/// Shown when the signed-in user doesn't belong to any household yet.
struct HouseholdOnboardingView: View {
    @Environment(AuthSession.self) private var session
    @Environment(\.appConfiguration) private var configuration
    @ScaledMetric(relativeTo: .largeTitle) private var markSize: CGFloat = 64
    @State private var isConfirmingSignOut = false

    var body: some View {
        NavigationStack {
            List {
                Section {
                    header
                        .listRowBackground(Color.clear)
                }
                Section {
                    NavigationLink {
                        CreateHouseholdForm()
                    } label: {
                        option(
                            "Create a Household",
                            detail: "You'll be its admin and can invite the people you cook with.",
                            systemImage: "house.fill")
                    }
                    NavigationLink {
                        JoinHouseholdForm()
                    } label: {
                        option(
                            "Join with a Code",
                            detail: "Enter the code from your invitation email or from the person who invited you.",
                            systemImage: "envelope.open.fill")
                    }
                }
            }
            .navigationTitle(configuration.displayName)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Menu {
                        if let user = session.currentUser {
                            Text(user.primaryEmail ?? user.displayName)
                        }
                        Button("Sign Out", systemImage: "rectangle.portrait.and.arrow.right", role: .destructive) {
                            isConfirmingSignOut = true
                        }
                    } label: {
                        Label("Account", systemImage: "person.crop.circle")
                    }
                }
            }
            .confirmationDialog(
                "Sign out of \(configuration.displayName)?",
                isPresented: $isConfirmingSignOut,
                titleVisibility: .visible
            ) {
                Button("Sign Out", role: .destructive) {
                    Task { await session.signOut() }
                }
                Button("Cancel", role: .cancel) {}
            }
        }
    }

    private var header: some View {
        VStack(spacing: 12) {
            Image(systemName: "person.2.circle.fill")
                .font(.system(size: markSize))
                .foregroundStyle(.tint)
                .accessibilityHidden(true)
            Text("Set Up Your Household")
                .font(.title2.bold())
                .accessibilityAddTraits(.isHeader)
            Text(
                "A household is the people you plan dinners and shop with. Start one, or join one you were invited to."
            )
            .font(.subheadline)
            .foregroundStyle(.secondary)
        }
        .multilineTextAlignment(.center)
        .frame(maxWidth: .infinity)
        .padding(.vertical, 8)
    }

    private func option(_ title: LocalizedStringKey, detail: LocalizedStringKey, systemImage: String) -> some View {
        Label {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.headline)
                Text(detail)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        } icon: {
            Image(systemName: systemImage)
                .foregroundStyle(.tint)
        }
        .padding(.vertical, 6)
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    HouseholdOnboardingView()
        .environment(session)
        .environment(HouseholdStore.preview(session: session, phase: .needsHousehold))
}
