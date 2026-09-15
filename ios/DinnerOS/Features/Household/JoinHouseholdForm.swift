import SwiftUI

/// Joins a household with a typed invite code. The code is normalized (uppercase, no
/// spaces or dashes) and validated by the server. Never log it.
///
/// Like an invitation link, a code is checked first and joining asks for confirmation,
/// naming the household and who sent the invitation.
struct JoinHouseholdForm: View {
    var onJoined: (Household) -> Void = { _ in }

    @Environment(HouseholdStore.self) private var households
    @State private var code = ""
    @State private var isWorking = false
    @State private var errorMessage: String?
    /// Set once the code checks out; presents the confirmation.
    @State private var preview: InvitationPreview?
    @FocusState private var isCodeFocused: Bool

    private var normalizedCode: String {
        InviteCode.normalize(code)
    }

    var body: some View {
        Form {
            Section {
                TextField("XXXXX-XXXXX", text: $code)
                    .font(.title3.monospaced())
                    .textInputAutocapitalization(.characters)
                    .autocorrectionDisabled()
                    .keyboardType(.asciiCapable)
                    .submitLabel(.join)
                    .focused($isCodeFocused)
                    .onSubmit { Task { await checkCode() } }
                    .accessibilityLabel("Invite code")
            } header: {
                Text("Invite Code")
            } footer: {
                Text(
                    "Enter the 10-character code from your invitation. Joining shares your name with the household's members."
                )
            }
            if let errorMessage {
                Section {
                    FormErrorLabel(message: errorMessage)
                }
            }
        }
        .navigationTitle("Join a Household")
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                if isWorking {
                    ProgressView()
                } else {
                    Button("Join") {
                        Task { await checkCode() }
                    }
                    .disabled(normalizedCode.isEmpty)
                }
            }
        }
        .disabled(isWorking)
        .alert(preview?.joinTitle ?? "", isPresented: isConfirming, presenting: preview) { _ in
            Button("Join") {
                Task { await join() }
            }
            Button("Cancel", role: .cancel) {}
        } message: { preview in
            Text(preview.joinMessage)
        }
        .onAppear { isCodeFocused = true }
    }

    private var isConfirming: Binding<Bool> {
        Binding(
            get: { preview != nil },
            set: { presented in
                if !presented { preview = nil }
            })
    }

    private func checkCode() async {
        guard !normalizedCode.isEmpty, !isWorking else { return }
        await run {
            preview = try await households.previewInvitation(code: code)
        }
    }

    private func join() async {
        guard !normalizedCode.isEmpty, !isWorking else { return }
        await run {
            let household = try await households.acceptInvitation(code: code)
            onJoined(household)
        }
    }

    private func run(_ operation: () async throws -> Void) async {
        isWorking = true
        errorMessage = nil
        defer { isWorking = false }
        do {
            try await operation()
        } catch is CancellationError {
            // The screen went away.
        } catch let error where HouseholdStore.isInvalidInvitation(error) {
            errorMessage = String(
                localized: "This invitation is no longer valid. Check the code, or ask for a new invitation.")
        } catch {
            errorMessage = HouseholdStore.message(for: error)
        }
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        JoinHouseholdForm()
    }
    .environment(session)
    .environment(HouseholdStore.preview(session: session, phase: .needsHousehold))
}
