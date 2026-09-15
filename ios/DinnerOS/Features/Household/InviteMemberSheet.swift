import SwiftUI
import UIKit
import UniformTypeIdentifiers

/// Invites someone by email, then shows the one-time invite code to share.
struct InviteMemberSheet: View {
    /// Roles the current user may grant. The server re-checks the chosen role.
    let roles: [HouseholdRole]
    let householdName: String

    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss
    @State private var email = ""
    @State private var role: HouseholdRole
    @State private var isSending = false
    @State private var errorMessage: String?
    @State private var result: CreateInvitationResponse?

    init(roles: [HouseholdRole], householdName: String) {
        self.roles = roles
        self.householdName = householdName
        _role = State(initialValue: roles.contains(.member) ? .member : roles.first ?? .member)
    }

    private var trimmedEmail: String {
        email.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var body: some View {
        NavigationStack {
            Group {
                if let result {
                    InviteCreatedView(result: result, householdName: householdName)
                } else {
                    form
                }
            }
            .navigationTitle(result == nil ? "Invite Someone" : "Invitation")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                if result == nil {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel") { dismiss() }
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        if isSending {
                            ProgressView()
                        } else {
                            Button("Send") {
                                Task { await send() }
                            }
                            .disabled(trimmedEmail.isEmpty || roles.isEmpty)
                        }
                    }
                } else {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { dismiss() }
                    }
                }
            }
        }
        // Swiping away the code screen by accident would lose a code shown only once.
        .interactiveDismissDisabled(isSending || result != nil)
    }

    private var form: some View {
        Form {
            Section {
                TextField("Email address", text: $email)
                    .keyboardType(.emailAddress)
                    .textContentType(.emailAddress)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .submitLabel(.send)
                    .onSubmit { Task { await send() } }
            } footer: {
                Text("They'll get an email with a link and a code. Invitations expire after 7 days.")
            }
            if roles.isEmpty {
                Section {
                    Text("Your role can't invite people to this household.")
                        .foregroundStyle(.secondary)
                }
            } else {
                Section {
                    Picker("Role", selection: $role) {
                        ForEach(roles) { role in
                            Text(role.displayName).tag(role)
                        }
                    }
                } footer: {
                    Text(role.summary)
                }
            }
            if let errorMessage {
                Section {
                    FormErrorLabel(message: errorMessage)
                }
            }
        }
        .disabled(isSending)
    }

    private func send() async {
        guard !trimmedEmail.isEmpty, !roles.isEmpty, !isSending else { return }
        isSending = true
        errorMessage = nil
        defer { isSending = false }
        do {
            result = try await households.createInvitation(email: trimmedEmail, role: role)
        } catch is CancellationError {
            // The sheet went away.
        } catch {
            errorMessage = HouseholdStore.message(for: error)
        }
    }
}

/// The one-time invite code, with copy and share.
struct InviteCreatedView: View {
    let result: CreateInvitationResponse
    let householdName: String

    @Environment(\.appConfiguration) private var configuration
    @State private var didCopy = false

    var body: some View {
        Form {
            Section {
                VStack(spacing: 8) {
                    Image(systemName: result.emailDelivered ? "checkmark.circle.fill" : "exclamationmark.triangle.fill")
                        .font(.largeTitle)
                        .foregroundStyle(result.emailDelivered ? Color.accentColor : Color.orange)
                        .accessibilityHidden(true)
                    Text(result.emailDelivered ? "Invitation Sent" : "Email Not Sent")
                        .font(.title2.bold())
                    Text(
                        result.emailDelivered
                            ? "We emailed \(result.invitation.email) a link to join."
                            : "We couldn't email \(result.invitation.email). The invitation still works: share the code below."
                    )
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                }
                .multilineTextAlignment(.center)
                .frame(maxWidth: .infinity)
                .listRowBackground(Color.clear)
                .accessibilityElement(children: .combine)
            }
            Section {
                Text(result.code)
                    .font(.largeTitle.monospaced().weight(.semibold))
                    .minimumScaleFactor(0.5)
                    .lineLimit(1)
                    .frame(maxWidth: .infinity)
                    .textSelection(.enabled)
                    .speechSpellsOutCharacters()
                Button {
                    copyCode()
                } label: {
                    Label(didCopy ? "Copied" : "Copy Code", systemImage: didCopy ? "checkmark" : "doc.on.doc")
                }
                ShareLink(item: shareMessage) {
                    Label("Share Code", systemImage: "square.and.arrow.up")
                }
            } header: {
                Text("Invite Code")
            } footer: {
                Text(
                    "This code is shown only once. One person can use it within 7 days, so share it only with \(result.invitation.email)."
                )
            }
        }
        .sensoryFeedback(.success, trigger: didCopy) { _, copied in copied }
    }

    private var shareMessage: String {
        String(
            localized:
                "Join “\(householdName)” on \(configuration.displayName): choose Join with a Code and enter \(result.code)"
        )
    }

    private func copyCode() {
        // The code expires from the clipboard so it doesn't linger there.
        UIPasteboard.general.setItems(
            [[UTType.plainText.identifier: result.code]],
            options: [.expirationDate: Date.now.addingTimeInterval(10 * 60)])
        didCopy = true
    }
}

#Preview("Form") {
    let session = HouseholdPreviewData.session()
    InviteMemberSheet(roles: HouseholdRole.assignable, householdName: "The Lovelace Kitchen")
        .environment(session)
        .environment(HouseholdPreviewData.store(session: session))
}

#Preview("Code, email failed") {
    NavigationStack {
        InviteCreatedView(
            result: CreateInvitationResponse(
                invitation: HouseholdPreviewData.invitations[0], code: "7K2QX-M9D4P", emailDelivered: false),
            householdName: "The Lovelace Kitchen")
    }
}
