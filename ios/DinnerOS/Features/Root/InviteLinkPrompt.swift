import SwiftUI

extension View {
    /// Asks before joining through an invitation link, then reports the result.
    func inviteLinkPrompt() -> some View {
        modifier(InviteLinkPrompt())
    }
}

/// Opening a link never joins a household silently: a link from someone you don't know
/// would otherwise add you to their household and share your name with its members.
private struct InviteLinkPrompt: ViewModifier {
    @Environment(HouseholdStore.self) private var households

    func body(content: Content) -> some View {
        content
            .overlay {
                if households.inviteStatus == .accepting {
                    ProgressView("Joining household…")
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

    private var isPresented: Binding<Bool> {
        Binding(
            get: {
                switch households.inviteStatus {
                case .awaitingConfirmation, .joined, .failed: true
                case .accepting, nil: false
                }
            },
            set: { presented in
                // Every button updates the status itself; this covers other dismissals.
                if !presented { households.dismissInviteStatus() }
            })
    }

    private var title: String {
        switch households.inviteStatus {
        case .awaitingConfirmation: String(localized: "Join Household?")
        case .joined: String(localized: "You're In")
        case .failed: String(localized: "Couldn't Join Household")
        case .accepting, nil: ""
        }
    }

    private var message: String {
        switch households.inviteStatus {
        case .awaitingConfirmation:
            String(
                localized:
                    "You opened an invitation link. Joining adds you to that household and shares your name with its members."
            )
        case .joined(let name):
            String(localized: "You joined \(name).")
        case .failed(let message):
            message
        case .accepting, nil:
            ""
        }
    }
}
