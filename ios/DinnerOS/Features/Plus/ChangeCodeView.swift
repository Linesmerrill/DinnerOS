import SwiftUI

/// Pick a custom invite code ("lines-fam") or get a new random one (decision 699). The server
/// checks each code as it's typed. Either way the old code and link stop working for good, so
/// both ask first.
struct ChangeCodeView: View {
    @Environment(PlusStore.self) private var plus
    @Environment(\.dismiss) private var dismiss

    @State private var custom = ""
    @State private var check: Check = .idle
    @State private var pending: Change?
    @State private var isSaving = false
    @State private var saveError: String?

    private enum Check: Equatable {
        case idle
        case checking
        case result(ReferralCodeAvailability)
        case failed(String)
    }

    private enum Change: Equatable {
        case custom(String)
        case random
    }

    var body: some View {
        Form {
            Section {
                TextField("Your own code", text: $custom, prompt: Text("lines-fam"))
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .submitLabel(.done)
                    .onChange(of: custom) { _, new in
                        // Codes are lowercase; show them that way as they're typed.
                        let lower = new.lowercased()
                        if lower != new { custom = lower }
                    }
                    .onSubmit(useCustom)
                    .accessibilityIdentifier("CustomCode")
                checkLine
                Button("Use This Code", action: useCustom)
                    .disabled(!canUseCustom || isSaving)
            } header: {
                Text("Pick Your Own")
            } footer: {
                Text(plus.referral?.codeHelp ?? String(localized: "Use 3 to 20 letters, numbers or hyphens."))
            }

            Section {
                Button("Get a New Random Code") { pending = .random }
                    .disabled(isSaving || noChangesLeft)
            } footer: {
                if let text = plus.referral?.codeChangesText {
                    Text(text)
                }
            }

            if let saveError {
                Section {
                    FormErrorLabel(message: saveError)
                }
            }
        }
        .dsReadableForm()
        .navigationTitle("Change Your Code")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) {
                Button("Cancel") { dismiss() }
            }
        }
        .task(id: trimmed) { await checkCode() }
        .alert(
            "Replace your code?",
            isPresented: Binding(get: { pending != nil }, set: { if !$0 { pending = nil } }),
            presenting: pending
        ) { change in
            Button("Replace Code") { save(change) }
            Button("Cancel", role: .cancel) {}
        } message: { _ in
            Text("Your old code and link will stop working.")
        }
    }

    @ViewBuilder
    private var checkLine: some View {
        switch check {
        case .idle:
            EmptyView()
        case .checking:
            Label("Checking…", systemImage: "ellipsis")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        case .result(let result):
            Label(result.message, systemImage: result.available ? "checkmark.circle.fill" : "xmark.circle.fill")
                .font(.subheadline)
                .foregroundStyle(result.available ? Color.accentColor : Color.red)
                .accessibilityIdentifier("CodeCheck")
        case .failed(let message):
            Label(message, systemImage: "exclamationmark.triangle")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
    }

    private var trimmed: String {
        custom.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private var noChangesLeft: Bool {
        plus.referral?.codeChangesLeft == 0
    }

    private var canUseCustom: Bool {
        if case .result(let result) = check, result.available, !noChangesLeft { return true }
        return false
    }

    private func checkCode() async {
        let code = trimmed
        guard !code.isEmpty else {
            check = .idle
            return
        }
        // Wait for a pause in typing before asking.
        try? await Task.sleep(for: .milliseconds(350))
        guard !Task.isCancelled else { return }
        check = .checking
        do {
            let result = try await plus.codeAvailability(code)
            guard !Task.isCancelled, code == trimmed else { return }
            check = .result(result)
        } catch is CancellationError {
        } catch {
            guard !Task.isCancelled else { return }
            check = .failed(HouseholdStore.message(for: error))
        }
    }

    private func useCustom() {
        guard canUseCustom else { return }
        pending = .custom(trimmed)
    }

    private func save(_ change: Change) {
        guard !isSaving else { return }
        isSaving = true
        saveError = nil
        Task {
            defer { isSaving = false }
            do {
                switch change {
                case .custom(let code): try await plus.changeCode(custom: code)
                case .random: try await plus.changeCode(custom: nil)
                }
                dismiss()
            } catch {
                saveError = HouseholdStore.message(for: error)
            }
        }
    }
}
