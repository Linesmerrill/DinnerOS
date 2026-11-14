import SwiftUI

/// Invite another household, or use another household's code. Both get a free month of
/// Plus (decision 675).
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
                YourInvitesSection(invites: invites)
            }

            if let used = plus.referral?.invites?.usedCode {
                Section {
                    Label(PlusText.usedCode(used), systemImage: "gift.fill")
                }
            } else if redeemed {
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
        .navigationTitle("Give a Free Month")
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

/// Who used the household's code and what it earned. Other households are never named.
struct YourInvitesSection: View {
    let invites: PlusInvites

    var body: some View {
        Section {
            LabeledContent("Households joined", value: invites.joined.formatted())
            LabeledContent("Free months earned", value: invites.capText)
            if let free = invites.freeUntil {
                Label("Plus free until \(PlusText.day(free))", systemImage: "gift.fill")
            }
            if invites.joins.isEmpty {
                Text(invites.emptyText)
                    .foregroundStyle(.secondary)
            } else {
                ForEach(Array(invites.joins.enumerated()), id: \.offset) { _, join in
                    LabeledContent {
                        Text(join.status)
                            .foregroundStyle(join.counted ? Color.accentColor : Color.secondary)
                    } label: {
                        Text("A household joined, \(PlusText.day(join.at))")
                    }
                }
            }
        } header: {
            Text("Your Invites")
        }
    }
}

/// Copies the code without its space. The icon turns into a green check for a moment.
struct CopyCodeButton: View {
    let code: String
    @State private var copied = false
    @State private var copies = 0
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
        .sensoryFeedback(.impact(weight: .light), trigger: copies)
        .onDisappear { reset?.cancel() }
    }

    private func copy() {
        UIPasteboard.general.string = PlusText.copyableCode(code)
        copies += 1
        withAnimation(.easeOut(duration: 0.25)) { copied = true }
        AccessibilityNotification.Announcement(String(localized: "Copied")).post()
        reset?.cancel()
        reset = Task {
            try? await Task.sleep(for: .seconds(1.5))
            guard !Task.isCancelled else { return }
            withAnimation(.easeOut(duration: 0.25)) { copied = false }
        }
    }
}
