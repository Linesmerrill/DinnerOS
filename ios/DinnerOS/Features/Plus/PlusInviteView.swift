import SwiftUI

/// Invite another household, or use another household's code. Both get a free month of
/// Plus (decision 675).
struct PlusInviteView: View {
    @Environment(PlusStore.self) private var plus
    @State private var loadError: String?
    @State private var code = ""
    @State private var isRedeeming = false
    @State private var redeemError: String?
    @State private var redeemed = false

    var body: some View {
        Form {
            Section {
                if let referral = plus.referral {
                    VStack(alignment: .leading, spacing: 12) {
                        Text(referral.explainer)
                        Text(Self.spaced(referral.code))
                            .font(.title.monospaced().bold())
                            .textSelection(.enabled)
                            .accessibilityLabel("Your code")
                            .accessibilityValue(referral.code.map(String.init).joined(separator: " "))
                    }
                    .padding(.vertical, 4)
                    ShareLink(item: referral.shareText) {
                        Label("Share Your Code", systemImage: "square.and.arrow.up")
                    }
                } else if let loadError {
                    FormErrorLabel(message: loadError)
                } else {
                    ProgressView()
                }
            } header: {
                Text("Your Code")
            }

            if redeemed {
                Section {
                    Label("You both have a free month of Plus.", systemImage: "gift.fill")
                }
            } else {
                Section {
                    TextField("Code", text: $code)
                        .textInputAutocapitalization(.characters)
                        .autocorrectionDisabled()
                        .submitLabel(.done)
                        .onSubmit(redeem)
                    Button("Use Code", action: redeem)
                        .disabled(code.trimmingCharacters(in: .whitespaces).isEmpty || isRedeeming)
                    if let redeemError {
                        FormErrorLabel(message: redeemError)
                    }
                } header: {
                    Text("Got a Code?")
                } footer: {
                    Text("Enter another household's code to get a free month of Plus for both of you.")
                }
            }
        }
        .navigationTitle("Invite a Household")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    /// "ABCD EFGH": easier to read aloud.
    static func spaced(_ code: String) -> String {
        guard code.count == 8 else { return code }
        return code.prefix(4) + " " + code.suffix(4)
    }

    private func load() async {
        loadError = nil
        do {
            try await plus.loadReferral()
        } catch is CancellationError {
        } catch {
            loadError = HouseholdStore.message(for: error)
        }
    }

    private func redeem() {
        guard !isRedeeming else { return }
        redeemError = nil
        isRedeeming = true
        Task {
            defer { isRedeeming = false }
            do {
                try await plus.redeem(code: code)
                redeemed = true
            } catch {
                redeemError = HouseholdStore.message(for: error)
            }
        }
    }
}

/// The Household tab's Plus row, shown only while Plus is on.
struct PlusHouseholdSection: View {
    @Environment(PlusStore.self) private var plus

    var body: some View {
        if let status = plus.status, status.enabled {
            Section {
                if status.active {
                    Label(PlusText.status(status), systemImage: "checkmark.seal.fill")
                } else {
                    Button {
                        plus.showsPurchase = true
                    } label: {
                        Label("Get DinnerOS Plus", systemImage: "sparkles")
                    }
                }
                NavigationLink {
                    PlusInviteView()
                } label: {
                    Label("Invite a Household", systemImage: "gift")
                }
            } header: {
                Text("DinnerOS Plus")
            } footer: {
                Text("Invite another household and you both get a free month.")
            }
        }
    }
}
