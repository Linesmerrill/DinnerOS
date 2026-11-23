import SwiftUI

/// Shown when the signed-in user doesn't belong to any household yet.
struct HouseholdOnboardingView: View {
    @Environment(AuthSession.self) private var session
    @Environment(\.appConfiguration) private var configuration
    @Environment(PlusStore.self) private var plus
    @State private var isConfirmingSignOut = false

    var body: some View {
        NavigationStack {
            List {
                Section {
                    header
                        .listRowBackground(Color.clear)
                }
                // A friend's code from an invite link, already saved. There's no field to type one:
                // Join with a Code is the one way to join with a code here (build 121). A friend's
                // code can still be entered later, in Give a Free Month.
                if plus.savedInvite?.accepted == true {
                    InviteCodeSetupSection()
                }
                Section {
                    NavigationLink {
                        CreateHouseholdForm()
                    } label: {
                        option(
                            "Create a Household",
                            detail: "You’ll be its admin and can invite the people you cook with.",
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
                // Reachable before any household exists, so an account can always be deleted.
                Section("Account") {
                    FeedbackLink()
                    // The same way to reach us as on the Household tab (Hark's build 122 round 5).
                    ContactUsLink()
                    PrivacyPolicyLink()
                    DeleteAccountButton()
                }
            }
            .navigationTitle(configuration.displayName)
            .navigationBarTitleDisplayMode(.inline)
            .dsReadableForm()
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
        VStack(spacing: DS.Space.m) {
            Image(systemName: "person.2.circle.fill")
                .font(DS.Typography.largeTitle)
                .imageScale(.large)
                .foregroundStyle(DS.Colors.accent)
                .accessibilityHidden(true)
            Text("Set Up Your Household")
                .font(DS.Typography.title)
                .accessibilityAddTraits(.isHeader)
            Text(
                "A household is the people you plan dinners and shop with. Start one, or join one you were invited to."
            )
            .font(DS.Typography.subheadline)
            .foregroundStyle(DS.Colors.secondaryText)
        }
        .multilineTextAlignment(.center)
        .frame(maxWidth: .infinity)
        .padding(.vertical, DS.Space.s)
    }

    private func option(_ title: LocalizedStringKey, detail: LocalizedStringKey, systemImage: String) -> some View {
        DSIconRow(systemImage: systemImage) {
            VStack(alignment: .leading, spacing: DS.Space.hairline) {
                Text(title)
                    .font(DS.Typography.headline)
                Text(detail)
                    .font(DS.Typography.subheadline)
                    .foregroundStyle(DS.Colors.secondaryText)
            }
        }
        .padding(.vertical, DS.Space.xs)
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    HouseholdOnboardingView()
        .environment(session)
        .environment(HouseholdStore.preview(session: session, phase: .needsHousehold))
        .environment(PlusStore(session: session, api: nil, purchaser: StoreKitPlusPurchaser()))
}
