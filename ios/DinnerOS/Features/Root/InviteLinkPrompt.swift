import SwiftUI

extension View {
    /// Checks an invitation link, asks before joining, then reports the result.
    func inviteLinkPrompt() -> some View {
        modifier(InviteLinkPrompt())
    }
}

/// Opening a link never joins a household silently: a link from someone you don't know
/// would otherwise add you to their household and share your name with its members. The
/// prompt names the household and who sent the invitation, so people know what they're
/// joining.
private struct InviteLinkPrompt: ViewModifier {
    @Environment(HouseholdStore.self) private var households

    func body(content: Content) -> some View {
        content
            .overlay {
                if let progressMessage {
                    ProgressView(progressMessage)
                        .padding(24)
                        .background(.regularMaterial, in: .rect(cornerRadius: 16, style: .continuous))
                }
            }
            .alert(title, isPresented: isPresented) {
                switch households.inviteStatus {
                case .awaitingConfirmation:
                    Button("Join") { households.confirmPendingInvite() }
                    Button("Not Now", role: .cancel) { households.declinePendingInvite() }
                default:
                    Button("OK") { households.dismissInviteStatus() }
                }
            } message: {
                Text(message)
            }
    }

    private var progressMessage: String? {
        switch households.inviteStatus {
        case .loadingPreview: String(localized: "Checking invitation…")
        case .accepting: String(localized: "Joining household…")
        default: nil
        }
    }

    private var isPresented: Binding<Bool> {
        Binding(
            get: {
                switch households.inviteStatus {
                case .awaitingConfirmation, .invalid, .joined, .failed: true
                case .loadingPreview, .accepting, nil: false
                }
            },
            set: { presented in
                // Every button updates the status itself; this covers other dismissals.
                if !presented { households.dismissInviteStatus() }
            })
    }

    private var title: String {
        switch households.inviteStatus {
        case .awaitingConfirmation(let preview): preview.joinTitle
        case .invalid: String(localized: "This invitation is no longer valid")
        case .joined: String(localized: "You're In")
        case .failed: String(localized: "Couldn't Join Household")
        case .loadingPreview, .accepting, nil: ""
        }
    }

    private var message: String {
        switch households.inviteStatus {
        case .awaitingConfirmation(let preview):
            preview.joinMessage
        case .invalid:
            String(
                localized:
                    "It may have expired, been revoked, or already been used. Ask the person who invited you for a new invitation."
            )
        case .joined(let name):
            String(localized: "You joined \(name).")
        case .failed(let message):
            message
        case .loadingPreview, .accepting, nil:
            ""
        }
    }
}
