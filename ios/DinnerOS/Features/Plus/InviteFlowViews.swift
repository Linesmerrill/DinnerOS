import SwiftUI

// An invite link, the free month checklist, and the ways back to it (build 112).

// MARK: - The invite screen

extension View {
    /// Shows the invite screen over everything when an invite link opens the app, signed in
    /// or not.
    func inviteCodeScreen() -> some View {
        modifier(InviteCodeScreenPresenter())
    }
}

private struct InviteCodeScreenPresenter: ViewModifier {
    @Environment(PlusStore.self) private var plus

    func body(content: Content) -> some View {
        @Bindable var plus = plus
        content
            .fullScreenCover(item: $plus.incomingInvite) { invite in
                InviteCodeScreen(invite: invite)
            }
    }
}

/// "Lines invited you to DinnerOS", with the code filled in, Accept and Not Now. Someone
/// whose household is already set up is told codes are for new households instead.
struct InviteCodeScreen: View {
    let invite: IncomingInvite

    @Environment(PlusStore.self) private var plus
    @Environment(AuthSession.self) private var session
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dismiss) private var dismiss
    @ScaledMetric(relativeTo: .largeTitle) private var markSize: CGFloat = 56

    @State private var loaded: Loaded = .loading
    @State private var isOwnCode = false

    private enum Loaded: Equatable {
        case loading
        case preview(InvitePreview)
        case failed(String)
    }

    /// Who's opening it. Waits while sign-in and the household load, so a set-up household is
    /// never offered Accept for a moment.
    private enum Viewer {
        case waiting
        case newHousehold
        case existingHousehold
    }

    private var viewer: Viewer {
        switch session.state {
        case .restoring: return .waiting
        case .signedOut, .configurationError: return .newHousehold
        case .signedIn:
            switch households.phase {
            case .idle, .loading: return .waiting
            case .needsHousehold, .failed: return .newHousehold
            case .ready: return households.current == nil ? .newHousehold : .existingHousehold
            }
        }
    }

    var body: some View {
        NavigationStack {
            Group {
                switch (loaded, viewer) {
                case (.loading, _), (_, .waiting):
                    ProgressView()
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                case (.failed(let message), _):
                    notice(
                        title: String(localized: "That Invite Didn't Work"), message: message,
                        systemImage: "exclamationmark.circle", seeYourCode: false)
                case (.preview(let preview), .existingHousehold):
                    let shown = isOwnCode ? preview.forOwn : preview.forExisting
                    notice(title: shown.title, message: shown.message, systemImage: "house.fill", seeYourCode: true)
                case (.preview(let preview), .newHousehold):
                    welcome(preview)
                }
            }
            .navigationBarTitleDisplayMode(.inline)
            .navigationDestination(for: PlusInvitesRoute.self) { _ in
                PlusInviteView()
            }
        }
        .task { await load() }
        .accessibilityIdentifier("InviteCodeScreen")
    }

    // MARK: New household

    private func welcome(_ preview: InvitePreview) -> some View {
        ScrollView {
            VStack(spacing: 20) {
                Image(systemName: "gift.fill")
                    .font(.system(size: markSize))
                    .foregroundStyle(.tint)
                    .accessibilityHidden(true)
                VStack(spacing: 8) {
                    Text(preview.title)
                        .font(.title.bold())
                        .accessibilityAddTraits(.isHeader)
                    Text(preview.message)
                        .font(.body)
                        .foregroundStyle(.secondary)
                }
                .multilineTextAlignment(.center)
                codeBox
                steps(preview.steps)
            }
            .padding(24)
            .frame(maxWidth: 560)
            .frame(maxWidth: .infinity)
        }
        .safeAreaInset(edge: .bottom) {
            VStack(spacing: 4) {
                Button {
                    plus.acceptInvite(code: invite.code, title: preview.title)
                    dismiss()
                } label: {
                    Text("Accept")
                        .font(.headline)
                        .frame(maxWidth: .infinity, minHeight: 32)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                Button("Not Now") {
                    plus.declineInvite()
                    dismiss()
                }
                .frame(minHeight: 44)
            }
            .padding(.horizontal, 24)
            .padding(.top, 8)
            .frame(maxWidth: 560)
            .frame(maxWidth: .infinity)
            .background(.bar)
        }
    }

    private var codeBox: some View {
        VStack(spacing: 4) {
            Text("Your Invite Code")
                .font(.subheadline)
                .foregroundStyle(.secondary)
            Text(invite.code)
                .font(.title2.monospaced().bold())
                .textSelection(.enabled)
                .accessibilityIdentifier("InviteCodeValue")
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 14)
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 14))
        .accessibilityElement(children: .combine)
    }

    private func steps(_ steps: [String]) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Then do these to get your free month")
                .font(.subheadline.weight(.semibold))
            ForEach(Array(steps.enumerated()), id: \.offset) { index, step in
                HStack(alignment: .firstTextBaseline, spacing: 12) {
                    Text("\(index + 1)")
                        .font(.subheadline.weight(.bold))
                        .foregroundStyle(Color("OnAccent"))
                        .frame(minWidth: 24, minHeight: 24)
                        .background(Circle().fill(.tint))
                        .accessibilityHidden(true)
                    Text(step)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .accessibilityElement(children: .combine)
                .accessibilityLabel(Text("Step \(index + 1). ") + Text(step))
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    // MARK: Set up already, your own code, or a code that didn't work

    private func notice(title: String, message: String, systemImage: String, seeYourCode: Bool) -> some View {
        VStack(spacing: 20) {
            Spacer(minLength: 0)
            Image(systemName: systemImage)
                .font(.system(size: markSize))
                .foregroundStyle(.tint)
                .accessibilityHidden(true)
            VStack(spacing: 8) {
                Text(title)
                    .font(.title2.bold())
                    .accessibilityAddTraits(.isHeader)
                Text(message)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .multilineTextAlignment(.center)
            if seeYourCode {
                NavigationLink(value: PlusInvitesRoute()) {
                    Text("See Your Code")
                        .font(.headline)
                        .frame(maxWidth: .infinity, minHeight: 32)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
            }
            Button("Close") {
                plus.declineInvite()
                dismiss()
            }
            .frame(minHeight: 44)
            Spacer(minLength: 0)
        }
        .padding(24)
        .frame(maxWidth: 560)
        .frame(maxWidth: .infinity)
    }

    private func load() async {
        do {
            loaded = .preview(try await plus.invitePreview(invite.code))
        } catch is CancellationError {
            return
        } catch {
            loaded = .failed(HouseholdStore.message(for: error))
            // A code that doesn't work isn't worth offering again in setup.
            if let apiError = error as? APIError, apiError.status == 404 { plus.clearSavedInvite() }
            return
        }
        // A household that's already set up: the code isn't for it, so don't keep it.
        while viewer == .waiting {
            try? await Task.sleep(for: .milliseconds(200))
            if Task.isCancelled { return }
        }
        guard viewer == .existingHousehold else { return }
        plus.clearSavedInvite()
        if plus.referral == nil { try? await plus.loadReferral() }
        if let own = plus.referral?.code {
            isOwnCode = InviteCode.normalize(own) == InviteCode.normalize(invite.code)
        }
    }
}

// MARK: - Setup: "Got an Invite Code?"

/// Early in setup, before making a household: a code from a friend's link. It's filled in
/// when the app has one saved, and never read from the clipboard.
struct InviteCodeSetupSection: View {
    @Environment(PlusStore.self) private var plus

    @State private var code = ""
    @State private var isChecking = false
    @State private var error: String?

    var body: some View {
        Section {
            if let saved = plus.savedInvite, saved.accepted {
                Label {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(saved.title ?? String(localized: "Your invite code is saved"))
                            .font(.headline)
                        Text(
                            "Create your household next. Then add a recipe and plan a dinner, and you both get a free month of Plus."
                        )
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    }
                } icon: {
                    Image(systemName: "checkmark.circle.fill")
                        .foregroundStyle(.tint)
                }
                .accessibilityIdentifier("InviteCodeSaved")
                Button("Use a Different Code") {
                    code = ""
                    plus.clearSavedInvite()
                }
            } else {
                TextField("Invite code", text: $code)
                    .textInputAutocapitalization(.characters)
                    .autocorrectionDisabled()
                    .submitLabel(.done)
                    .onSubmit(check)
                    .accessibilityIdentifier("SetupInviteCode")
                Button(action: check) {
                    if isChecking {
                        ProgressView()
                    } else {
                        Text("Use This Code")
                    }
                }
                .disabled(code.trimmingCharacters(in: .whitespaces).isEmpty || isChecking)
                if let error {
                    FormErrorLabel(message: error)
                }
            }
        } header: {
            Text("Got an Invite Code?")
        } footer: {
            // Once a code is saved, the row above already says this.
            if plus.savedInvite?.accepted != true {
                Text("From a friend's DinnerOS link. You both get a free month of Plus.")
            }
        }
        .onAppear {
            if code.isEmpty, let saved = plus.savedInvite?.code { code = saved }
        }
    }

    private func check() {
        let typed = InviteCode.normalize(code)
        guard !typed.isEmpty, !isChecking else { return }
        error = nil
        isChecking = true
        Task {
            defer { isChecking = false }
            do {
                let preview = try await plus.invitePreview(typed)
                plus.acceptInvite(code: typed, title: preview.title)
            } catch is CancellationError {
            } catch {
                self.error = HouseholdStore.message(for: error)
            }
        }
    }
}

// MARK: - The checklist

/// "Create your household", "Add a recipe: ...", "Plan a dinner": a check that fills in as
/// each is done, and a button that goes to where you do it. Tapping the row does the same.
/// `onGo` runs first, to close a sheet.
struct ReferralChecklist: View {
    let progress: ReferralProgress
    var onGo: (() -> Void)?

    /// Optional: the invite screen over sign-in has no tabs to go to.
    @Environment(TabRouter.self) private var tabs: TabRouter?

    var body: some View {
        ForEach(progress.steps) { step in
            HStack(spacing: 12) {
                Image(systemName: step.done ? "checkmark.circle.fill" : "circle")
                    .foregroundStyle(step.done ? Color.accentColor : Color.secondary)
                    .imageScale(.large)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text(step.title)
                        .foregroundStyle(step.done ? Color.secondary : Color.primary)
                    if let count = step.count, let target = step.target, target > 1, !step.done {
                        Text("\(count) of \(target)")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                }
                Spacer(minLength: 8)
                if canGo(step), let tabs, let label = Self.goLabel(step.id) {
                    Button(label) { go(step.id, tabs: tabs) }
                        .buttonStyle(.bordered)
                        .controlSize(.small)
                        .accessibilityIdentifier("ChecklistGo-\(step.id)")
                }
            }
            .padding(.vertical, 2)
            .contentShape(.rect)
            .onTapGesture {
                guard canGo(step), let tabs else { return }
                go(step.id, tabs: tabs)
            }
            .accessibilityElement(children: .contain)
            .accessibilityLabel(Text(step.title))
            .accessibilityValue(step.done ? Text("Done") : Text("Not done yet"))
            .accessibilityIdentifier("ChecklistStep-\(step.id)")
        }
    }

    private func canGo(_ step: ReferralProgress.Step) -> Bool {
        !step.done && progress.isPending && Self.goLabel(step.id) != nil
    }

    static func goLabel(_ id: String) -> String? {
        switch id {
        case "recipes": String(localized: "Add a Recipe")
        case "dinner": String(localized: "Plan a Dinner")
        default: nil
        }
    }

    private func go(_ id: String, tabs: TabRouter) {
        onGo?()
        switch id {
        case "recipes":
            // The Add a Recipe choices, which include Shared Recipes (decision 706).
            tabs.selection = .menu
            tabs.menuPath = NavigationPath()
            tabs.addRecipeRequested = true
        case "dinner":
            tabs.selection = .menu
            tabs.menuPath = NavigationPath()
            tabs.addMealsRequested = true
        default:
            break
        }
    }
}

/// The checklist's heading: the message and how long is left.
struct ReferralChecklistHeader: View {
    let progress: ReferralProgress

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(progress.summaryText)
                .font(.headline)
            Text(progress.message)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            if let timeLeft = progress.timeLeft {
                Label(timeLeft, systemImage: "clock")
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.orange)
                    .accessibilityIdentifier("ChecklistTimeLeft")
            }
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .combine)
    }
}

/// The checklist on its own, opened from the Menu card or right after the code goes in.
struct ReferralChecklistSheet: View {
    @Environment(PlusStore.self) private var plus
    @Environment(\.dismiss) private var dismiss

    private var progress: ReferralProgress? {
        plus.pendingReferral ?? plus.status?.referralProgress
    }

    var body: some View {
        NavigationStack {
            List {
                if let progress {
                    if progress.isPending {
                        Section {
                            PlusExplainer(progress: progress)
                        }
                    }
                    Section {
                        ReferralChecklistHeader(progress: progress)
                    }
                    if progress.isPending {
                        Section {
                            ReferralChecklist(progress: progress) { dismiss() }
                        }
                    }
                } else {
                    ProgressView()
                        .frame(maxWidth: .infinity)
                }
            }
            .navigationTitle("Your Free Month")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    // While there's still something to do, closing it is putting it off.
                    if progress?.isPending == true {
                        Button("Not Now") { dismiss() }
                            .accessibilityIdentifier("ChecklistNotNow")
                    } else {
                        Button("Done") { dismiss() }
                    }
                }
            }
            .task { await plus.reload() }
        }
        .accessibilityIdentifier("ReferralChecklistSheet")
    }
}

/// The checklist in Give a Free Month, until the code counts.
struct ReferralChecklistSection: View {
    let progress: ReferralProgress

    var body: some View {
        // What Plus is goes in its own section, so the checklist's identifier doesn't cover it.
        if progress.isPending {
            Section {
                PlusExplainer(progress: progress)
            } header: {
                Text("Your Free Month")
            }
        }
        Section {
            ReferralChecklistHeader(progress: progress)
            ReferralChecklist(progress: progress)
        } header: {
            if !progress.isPending {
                Text("Your Free Month")
            }
        }
        .accessibilityIdentifier("ReferralProgress")
    }
}

// MARK: - The Menu card

/// At the top of the Menu until the free month is on or the invite expires: "Free month: 1
/// of 3 done". Opens the checklist. Hide puts it away here; Give a Free Month keeps it.
struct ReferralMenuCard: View {
    @Environment(PlusStore.self) private var plus
    @Environment(RecipeLibrary.self) private var library
    @Environment(PlanStore.self) private var plans

    var body: some View {
        Group {
            if plus.showsReferralCard, let progress = plus.pendingReferral {
                card(progress)
                    .padding(.horizontal, 16)
            }
        }
        // Saving recipes and planning a dinner change the count; check again when they might
        // have. The server counts a new dinner just after it's saved, so wait a moment.
        .task(id: SetupCounts(recipes: library.items.count, meals: plans.plan?.entries.count ?? 0)) {
            guard plus.pendingReferral != nil else { return }
            try? await Task.sleep(for: .seconds(1))
            guard !Task.isCancelled else { return }
            await plus.reload()
        }
    }

    private struct SetupCounts: Equatable {
        let recipes: Int
        let meals: Int
    }

    private func card(_ progress: ReferralProgress) -> some View {
        HStack(alignment: .center, spacing: 12) {
            Button {
                plus.showsChecklist = true
            } label: {
                HStack(spacing: 12) {
                    Image(systemName: "gift.fill")
                        .font(.title2)
                        .foregroundStyle(.tint)
                        .accessibilityHidden(true)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(progress.summaryText)
                            .font(.headline)
                            .foregroundStyle(Color.primary)
                        if let timeLeft = progress.timeLeft {
                            Text(timeLeft)
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                        }
                    }
                    Spacer(minLength: 4)
                    Image(systemName: "chevron.forward")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(.tertiary)
                        .accessibilityHidden(true)
                }
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityHint("Shows what's left for your free month")
            .accessibilityIdentifier("ReferralMenuCard")
            Button("Hide") { plus.hideReferralCard() }
                .font(.subheadline)
                .buttonStyle(.borderless)
                .accessibilityHint("You can still find it in Give a Free Month")
        }
        .padding(14)
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 16))
    }
}

// MARK: - The free month is on

/// Shown once, when the checklist is done and the free month starts.
struct FreeMonthOnView: View {
    let progress: ReferralProgress
    let done: () -> Void

    @Environment(PlusStore.self) private var plus
    @ScaledMetric(relativeTo: .largeTitle) private var markSize: CGFloat = 56

    var body: some View {
        ZStack {
            Color.black.opacity(0.35)
                .ignoresSafeArea()
                .accessibilityHidden(true)
            VStack(spacing: 16) {
                Image(systemName: "gift.fill")
                    .font(.system(size: markSize))
                    .foregroundStyle(.tint)
                    .accessibilityHidden(true)
                Text(progress.message)
                    .font(.title2.bold())
                    .multilineTextAlignment(.center)
                    .accessibilityAddTraits(.isHeader)
                if let until = plus.status?.freeUntil {
                    Text("Plus is free until \(PlusText.day(until)).")
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                }
                Button(action: done) {
                    Text("Done")
                        .font(.headline)
                        .frame(maxWidth: .infinity, minHeight: 32)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                .padding(.top, 4)
            }
            .padding(24)
            .frame(maxWidth: 420)
            // White in light; a raised gray in dark, so it stands off the dimmed app.
            .background(Color(.tertiarySystemBackground), in: .rect(cornerRadius: 24))
            .shadow(color: .black.opacity(0.2), radius: 20, y: 8)
            .padding(24)
            .accessibilityElement(children: .contain)
            .accessibilityAddTraits(.isModal)
            .accessibilityIdentifier("FreeMonthOn")
        }
    }
}
