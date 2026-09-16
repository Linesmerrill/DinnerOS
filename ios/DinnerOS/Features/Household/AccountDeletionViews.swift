import SwiftUI

/// "Delete Account", with its destructive confirmation (App Review guideline 5.1.1(v)).
///
/// The dialog is attached to the button itself: on iOS 26 a confirmation dialog is a popover
/// that points at the view it's attached to. On success the session signs out and the root
/// view shows sign-in, so there's nothing else to present.
struct DeleteAccountButton: View {
    @Environment(AuthSession.self) private var session
    @Environment(\.appConfiguration) private var configuration

    @State private var isConfirming = false
    @State private var isDeleting = false
    @State private var errorMessage: String?

    var body: some View {
        Button(role: .destructive) {
            isConfirming = true
        } label: {
            HStack {
                Text("Delete Account")
                if isDeleting {
                    Spacer()
                    ProgressView()
                }
            }
        }
        .disabled(isDeleting)
        .accessibilityHint("Permanently deletes your \(configuration.displayName) account.")
        .confirmationDialog(
            "Delete your \(configuration.displayName) account?",
            isPresented: $isConfirming,
            titleVisibility: .visible
        ) {
            Button("Delete Account", role: .destructive) { delete() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text(
                "This permanently deletes your account and ratings, and any household where you're the only member, with its plans, recipes, and pantry. Households you share stay with the other members; if you're a household's only admin, its longest-standing member becomes admin. This can't be undone."
            )
        }
        .alert(
            "Couldn't Delete Your Account",
            isPresented: Binding(presenting: $errorMessage),
            presenting: errorMessage
        ) { _ in
            Button("OK") {}
        } message: { message in
            Text(message)
        }
    }

    private func delete() {
        Task {
            isDeleting = true
            defer { isDeleting = false }
            do {
                try await session.deleteAccount()
            } catch is CancellationError {
                // Nothing to report.
            } catch {
                guard session.currentUser != nil else { return }
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }
}

/// A link to the privacy policy the API serves. Hidden when the build has no API URL.
struct PrivacyPolicyLink: View {
    @Environment(\.appConfiguration) private var configuration

    var body: some View {
        if let url = configuration.privacyPolicyURL {
            Link(destination: url) {
                Label("Privacy Policy", systemImage: "hand.raised")
            }
        }
    }
}
