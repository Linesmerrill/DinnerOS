import SwiftUI

/// Invite another household, or use another household's code. Both get a free month of
/// Plus once the new household plans a dinner and saves recipes (decisions 675 and 698).
struct PlusInviteView: View {
    @Environment(PlusStore.self) private var plus
    @State private var loadError: String?
    @State private var code: String

    /// `code` fills Got a Code, for a shared invite link.
    init(code: String = "") {
        _code = State(initialValue: code)
    }
    @State private var isRedeeming = false
    @State private var redeemError: String?
    @State private var redeemed = false

    var body: some View {
        Form {
            // A household that entered a code sees what's left first.
            if let progress = pendingProgress {
                ReferralProgressSection(progress: progress)
            }
            if let howItWorks = plus.referral?.howItWorks {
                HowItWorksSection(howItWorks: howItWorks)
            }
            Section {
                if let referral = plus.referral {
                    VStack(alignment: .leading, spacing: 12) {
                        Text(referral.explainer)
                        HStack(spacing: 12) {
                            Text(Self.spaced(referral.code))
                                .font(.title.monospaced().bold())
                                .textSelection(.enabled)
                                .accessibilityLabel("Your code")
                                .accessibilityValue(referral.code.map(String.init).joined(separator: " "))
                            CopyCodeButton(code: referral.code)
                        }
                    }
                    .padding(.vertical, 4)
                    if let url = referral.shareURL, let message = referral.shareMessage {
                        // The link goes as a link, so the share sheet and Messages show its card.
                        ShareLink(
                            item: url, message: Text(message),
                            preview: SharePreview(
                                Text("Get a free month of DinnerOS Plus"), image: Image("ShareIcon"))
                        ) {
                            Label("Share Your Code", systemImage: "square.and.arrow.up")
                        }
                    } else {
                        ShareLink(item: referral.shareText) {
                            Label("Share Your Code", systemImage: "square.and.arrow.up")
                        }
                    }
                } else if let loadError {
                    FormErrorLabel(message: loadError)
                } else {
                    ProgressView()
                }
            } header: {
                Text("Your Code")
            }

            if let invites = plus.referral?.invites {
                YourInvitesList(invites: invites)
            }

            if pendingProgress != nil {
                // Shown at the top.
            } else if let progress = plus.referral?.referralProgress ?? plus.status?.referralProgress,
                progress.state == "expired"
            {
                ReferralProgressSection(progress: progress)
            } else if let used = plus.referral?.invites?.usedCode {
                Section {
                    Label(PlusText.usedCode(used), systemImage: "gift.fill")
                }
            } else if redeemed {
                Section {
                    Label(
                        "Code saved. Plan a dinner and save a few recipes to unlock your free month.",
                        systemImage: "gift")
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
                    Text(
                        "Enter another household's code. Once you plan a dinner and save a few recipes, you both get a free month of Plus."
                    )
                }
            }
        }
        .navigationTitle("Give a Free Month")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    /// This household's invite, while it's still waiting on setup.
    private var pendingProgress: ReferralProgress? {
        let progress = plus.referral?.referralProgress ?? plus.status?.referralProgress
        return progress?.isPending == true ? progress : nil
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

/// Copies the code without its space. The icon turns into a green check for a moment.
struct CopyCodeButton: View {
    let code: String
    @State private var copied = false
    @State private var reset: Task<Void, Never>?

    var body: some View {
        Button(action: copy) {
            ZStack {
                if copied {
                    Image(systemName: "checkmark.circle.fill")
                        .foregroundStyle(.green)
                        .transition(.scale(scale: 0.5).combined(with: .opacity))
                } else {
                    Image(systemName: "doc.on.doc")
                        .foregroundStyle(.tint)
                        .transition(.scale(scale: 0.5).combined(with: .opacity))
                }
            }
            .font(.title2)
            .frame(minWidth: 44, minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.borderless)
        .accessibilityLabel("Copy code")
        .onDisappear { reset?.cancel() }
    }

    private func copy() {
        Clipboard.copy(PlusText.copyableCode(code))
        withAnimation(.easeOut(duration: 0.25)) { copied = true }
        reset?.cancel()
        reset = Task {
            try? await Task.sleep(for: .seconds(1.5))
            guard !Task.isCancelled else { return }
            withAnimation(.easeOut(duration: 0.25)) { copied = false }
        }
    }
}
