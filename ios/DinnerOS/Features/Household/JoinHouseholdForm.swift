import SwiftUI

/// Joins a household with a typed invite code. The code is normalized (uppercase, no
/// spaces or dashes) and validated by the server. Never log it.
struct JoinHouseholdForm: View {
    var onJoined: (Household) -> Void = { _ in }

    @Environment(HouseholdStore.self) private var households
    @State private var code = ""
    @State private var isJoining = false
    @State private var errorMessage: String?
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
                    .onSubmit { Task { await join() } }
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
                if isJoining {
                    ProgressView()
                } else {
                    Button("Join") {
                        Task { await join() }
                    }
                    .disabled(normalizedCode.isEmpty)
                }
            }
        }
        .disabled(isJoining)
        .onAppear { isCodeFocused = true }
    }

    private func join() async {
        guard !normalizedCode.isEmpty, !isJoining else { return }
        isJoining = true
        errorMessage = nil
        defer { isJoining = false }
        do {
            let household = try await households.acceptInvitation(code: code)
            onJoined(household)
        } catch is CancellationError {
            // The screen went away.
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
