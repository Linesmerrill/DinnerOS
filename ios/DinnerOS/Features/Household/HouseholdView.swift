import SwiftUI

/// The Household tab: a header card, then one row per screen (members, giving a free month,
/// Plus, planning and settings, other households, the account). Joining this household and
/// giving another household a free month are separate rows with separate words, so the two
/// invites can't be mixed up.
///
/// Settings, on the Planning screen, are changed in place and save themselves
/// (`HouseholdSettingsSections`); there is no Edit mode.
///
/// Actions the user's role doesn't allow are hidden. That's a convenience only: the API
/// enforces every permission and answers `403`/`409` if a hidden rule applies.
struct HouseholdView: View {
    @Environment(AuthSession.self) private var session
    @Environment(HouseholdStore.self) private var households
    @Environment(ImportReviewStore.self) private var importReviews
    @Environment(\.appConfiguration) private var configuration
    @Environment(PushNotificationStore.self) private var push: PushNotificationStore?
    @Environment(\.openURL) private var openURL
    @Environment(ShoppingStore.self) private var shopping: ShoppingStore?
    @Environment(PlusStore.self) private var plus: PlusStore?
    /// Optional so previews needn't supply one.
    @Environment(NextStepsStore.self) private var nextSteps: NextStepsStore?
    @Environment(BiometricLock.self) private var biometricLock: BiometricLock?

    @State private var me: MeResponse?
    /// Every household's store requests; `nil` for anyone who doesn't run DinnerOS.
    @State private var storeRequests: StoreRequestSummary?
    /// Every household's referrals; `nil` for anyone who doesn't run DinnerOS.
    @State private var referrals: PlusReferralSummary?
    @State private var isLoadingAccount = false
    @State private var accountError: String?

    @State private var sheet: Sheet?
    @State private var memberPendingRemoval: HouseholdMember?
    @State private var pendingRoleChange: RoleChange?
    @State private var invitationPendingRevoke: HouseholdInvitation?
    @State private var isConfirmingLeave = false
    @State private var isConfirmingSignOut = false
    @State private var isWorking = false
    @State private var actionError: String?

    private enum Sheet: String, Identifiable {
        case invite
        case createHousehold
        case joinHousehold

        var id: String { rawValue }
    }

    private struct RoleChange {
        let member: HouseholdMember
        let role: HouseholdRole
    }

    private var currentUserID: String {
        session.currentUser?.id ?? ""
    }

    var body: some View {
        List {
            if let detail = households.current {
                if let refreshError = households.refreshError {
                    Section {
                        FormErrorLabel(message: refreshError)
                    }
                }
                Section {
                    HouseholdHeaderCard(detail: detail, plusLine: plusLine)
                        .listRowInsets(EdgeInsets())
                        .listRowBackground(Color.clear)
                }
                .noTopSectionMargin()
                Section {
                    NavigationLink {
                        page("Members") {
                            membersSection(detail)
                            if detail.access.can(.membersInvite) {
                                invitationsSection(detail)
                            }
                        }
                    } label: {
                        DSIconRow(systemImage: "person.2") { Text("Members") }
                            .badge(detail.members?.count ?? 0)
                    }
                    if let plus, plus.isEnabled {
                        NavigationLink {
                            PlusInviteView()
                        } label: {
                            DSIconRow(systemImage: "gift") { Text("Give a Free Month") }
                                .nextStepDot(.giveFreeMonth)
                        }
                        plusRow(plus)
                    }
                } footer: {
                    if plus?.isEnabled == true {
                        Text(
                            "Members share this household. Give a Free Month is for a different household: once they set up, you both get a month of Plus."
                        )
                    }
                }
                Section {
                    NavigationLink {
                        page("Planning") {
                            Section {
                                NavigationLink {
                                    AutopilotPreferencesView()
                                } label: {
                                    DSIconRow(systemImage: "sparkles") { Text("Autopilot Preferences") }
                                        .nextStepDot(.autopilotPreferences)
                                }
                                // Import bookkeeping follows `recipes.import`, and hides against an
                                // API without the route.
                                if detail.access.can(.recipesImport), importReviews.isAvailable {
                                    NavigationLink {
                                        ImportReviewView()
                                    } label: {
                                        DSIconRow(systemImage: "tray.full") { Text("Import Review") }
                                            .nextStepDot(.importReview)
                                    }
                                }
                            } footer: {
                                Text("What your household likes, your schedule, and recipes we weren’t sure about.")
                            }
                            if let settings = households.settings {
                                HouseholdSettingsSections(settings: settings, role: detail.role)
                            }
                        }
                        .householdSettingsAutosave(households.settings)
                    } label: {
                        DSIconRow(systemImage: "calendar") { Text("Planning") }
                            .nextStepDot(.planning)
                    }
                    NavigationLink {
                        page("Households") {
                            if households.households.count > 1 {
                                switcherSection(detail)
                            }
                            moreHouseholdsSection
                            leaveSection
                        }
                    } label: {
                        DSIconRow(systemImage: "house") { Text("Households") }
                    }
                }
            }
            Section {
                NavigationLink {
                    page("Account") {
                        accountSection
                        signOutSection
                        Section {
                            DeleteAccountButton()
                        } footer: {
                            Text("Deletes your account and any household where you’re the only member.")
                        }
                    }
                } label: {
                    DSIconRow(systemImage: "person.crop.circle") { Text("Account") }
                }
            }
            if storeRequests != nil || referrals != nil {
                operatorSection
            }
        }
        // No tall empty band between the bar and the household card: the list's own top
        // spacing is gap enough (build 122, Hark's review item 16: too large on the Duo).
        .listSectionSpacing(.compact)
        .contentMargins(.top, 0, for: .scrollContent)
        .dsReadableForm()
        // The shared tab header: a small title in the bar, with the household's name one step
        // down in the card, so the tab never shows two large titles (build 121).
        .tabBarEdge()
        .tabHeader(String(localized: "Household"))
        .notificationsToolbar()
        .task { await loadAccount() }
        // Each time the tab shows, including coming back from a step just finished.
        .task { await nextSteps?.load() }
        .task { storeRequests = try? await shopping?.storeRequestSummary() ?? nil }
        .task { referrals = try? await plus?.referralSummary() ?? nil }
        // Only for someone who could act on it: without `recipes.import` the row is hidden and
        // the request would be a guaranteed `403`.
        .task(id: households.current?.household.id) {
            guard let householdID = households.current?.household.id,
                households.access?.can(.recipesImport) == true
            else { return }
            importReviews.activate(householdID: householdID)
            await importReviews.load()
        }
        .refreshable {
            await households.load()
            await loadAccount()
        }
    }

    /// One of the Household tab's screens, with the sheets and confirmations its rows use.
    private func page<Content: View>(_ title: LocalizedStringKey, @ViewBuilder content: () -> Content) -> some View {
        List {
            content()
        }
        .dsReadableForm()
        .navigationTitle(title)
        .disabled(isWorking)
        .overlay {
            if isWorking {
                ProgressView()
                    .controlSize(.large)
            }
        }
        .refreshable { await households.load() }
        .dsSheet(item: $sheet, content: sheetContent)
        .modifier(confirmations)
        .alert(
            "Couldn’t Complete That",
            isPresented: Binding(presenting: $actionError),
            presenting: actionError
        ) { _ in
            Button("OK") {}
        } message: { message in
            Text(message)
        }
    }

    /// The header card's Plus line, or nil while Plus is off. A comp says "on the house" only
    /// where `BuildChannel.showsCompWording` allows it, never in a Release build.
    private var plusLine: String? {
        guard let plus, plus.isEnabled, let status = plus.status else { return nil }
        guard status.active else { return String(localized: "No Plus Yet") }
        return PlusText.status(status, channel: BuildChannelStore.shared.channel)
    }

    /// A standard row like the others: the same icon column, text color and chevron, whether
    /// it opens Your Plus or the purchase screen.
    @ViewBuilder
    private func plusRow(_ plus: PlusStore) -> some View {
        if plus.status?.active == true {
            NavigationLink {
                YourPlusView()
            } label: {
                DSIconRow(systemImage: "checkmark.seal") { Text("DinnerOS Plus") }
            }
        } else {
            // The list's own disclosure chevron, in the same place as the other rows': a link
            // inside the button draws it, and the button opens the purchase sheet.
            Button {
                plus.showsPurchase = true
            } label: {
                NavigationLink(destination: EmptyView()) {
                    DSIconRow(systemImage: "sparkles") { Text("DinnerOS Plus") }
                }
                .foregroundStyle(Color.primary)
            }
        }
    }

    /// Off by default. Turning it on asks for a face first.
    private func biometricRow(_ lock: BiometricLock, kind: BiometryKind) -> some View {
        Toggle(
            isOn: Binding(
                get: { session.isBiometricSignInOn },
                set: { on in
                    if on {
                        Task { await lock.turnOn() }
                    } else {
                        lock.turnOff()
                    }
                })
        ) {
            DSIconRow(systemImage: kind.systemImage) { Text(kind.toggleTitle) }
        }
        .disabled(lock.isTurningOn)
        .accessibilityIdentifier("account.biometricSignIn")
    }

    private var signOutSection: some View {
        Section {
            Button("Sign Out", role: .destructive) {
                isConfirmingSignOut = true
            }
            .accessibilityHint("Signs you out of \(configuration.displayName) on this device.")
            // Attached to the button, not the list: on iOS 26 the dialog is a popover
            // that points at whatever view it's attached to.
            .confirmationDialog(
                "Sign out of \(configuration.displayName)?",
                isPresented: $isConfirmingSignOut,
                titleVisibility: .visible
            ) {
                Button("Sign Out", role: .destructive) { Task { await session.signOut() } }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("You’ll need to sign in again to see your household.")
            }
        }
    }

    private var operatorSection: some View {
        Section {
            if let storeRequests {
                NavigationLink {
                    StoreRequestsReportView(summary: storeRequests)
                } label: {
                    LabeledContent("Store Requests", value: storeRequests.households.formatted())
                }
            }
            if let referrals {
                NavigationLink {
                    ReferralsReportView(summary: referrals)
                } label: {
                    LabeledContent("Referrals", value: referrals.joinedAllTime.formatted())
                }
            }
        } header: {
            Text("Running DinnerOS")
        } footer: {
            Text("What households asked us to add, and who joined with a code. Only you can see this.")
        }
    }

    // MARK: - Sections

    private func switcherSection(_ detail: HouseholdDetail) -> some View {
        Section("Your Households") {
            ForEach(households.households) { item in
                let isSelected = item.id == detail.household.id
                Button {
                    Task { await households.selectHousehold(id: item.id) }
                } label: {
                    HStack {
                        VStack(alignment: .leading, spacing: DS.Space.hairline) {
                            Text(item.household.name)
                                .foregroundStyle(.primary)
                            Text(item.role.displayName)
                                .font(DS.Typography.subheadline)
                                .foregroundStyle(DS.Colors.secondaryText)
                        }
                        Spacer()
                        if isSelected {
                            Image(systemName: "checkmark")
                                .foregroundStyle(DS.Colors.accent)
                                .accessibilityHidden(true)
                        }
                    }
                    .contentShape(.rect)
                }
                .accessibilityAddTraits(isSelected ? .isSelected : [])
            }
        }
    }

    private func membersSection(_ detail: HouseholdDetail) -> some View {
        Section {
            if let members = detail.members {
                ForEach(members) { member in
                    HouseholdMemberRow(
                        member: member,
                        isCurrentUser: member.userID == currentUserID,
                        assignableRoles: detail.access.assignableRoles(for: member, currentUserID: currentUserID),
                        canRemove: detail.access.canRemove(member, currentUserID: currentUserID),
                        onChangeRole: { role in pendingRoleChange = RoleChange(member: member, role: role) },
                        onRemove: { memberPendingRemoval = member })
                }
            } else {
                Text("Your role can’t see the member list.")
                    .foregroundStyle(.secondary)
            }
        }
    }

    private func invitationsSection(_ detail: HouseholdDetail) -> some View {
        Section {
            Button {
                sheet = .invite
            } label: {
                DSIconRow(systemImage: "person.badge.plus") { Text("Add Someone") }
            }
            ForEach(households.invitations) { invitation in
                HStack {
                    VStack(alignment: .leading, spacing: DS.Space.hairline) {
                        Text(invitation.email)
                        Text(
                            "\(invitation.role.displayName) · Expires \(invitation.expiresAt.formatted(date: .abbreviated, time: .omitted))"
                        )
                        .font(DS.Typography.subheadline)
                        .foregroundStyle(DS.Colors.secondaryText)
                    }
                    .accessibilityElement(children: .combine)
                    Spacer()
                    Menu {
                        Button("Revoke Invitation", systemImage: "xmark.circle", role: .destructive) {
                            invitationPendingRevoke = invitation
                        }
                    } label: {
                        Image(systemName: "ellipsis.circle")
                            .imageScale(.large)
                            .accessibilityLabel("Manage invitation for \(invitation.email)")
                    }
                }
                .swipeActions {
                    Button("Revoke") { invitationPendingRevoke = invitation }
                        .tint(DS.Colors.destructive)
                }
            }
        } header: {
            Text("Invitations to Join")
        } footer: {
            Text(
                households.invitations.isEmpty
                    ? "No pending invitations. People you add here join this household and share its plans and lists."
                    : "People you add here join this household and share its plans and lists."
            )
        }
    }

    private var moreHouseholdsSection: some View {
        Section("Other Households") {
            Button {
                sheet = .createHousehold
            } label: {
                DSIconRow(systemImage: "house") { Text("Create Another Household") }
            }
            Button {
                sheet = .joinHousehold
            } label: {
                DSIconRow(systemImage: "envelope.open") { Text("Join with a Code") }
            }
        }
    }

    private var leaveSection: some View {
        Section {
            Button("Leave Household", role: .destructive) {
                isConfirmingLeave = true
            }
            .confirmationDialog(
                Text("Leave \(households.current?.household.name ?? "")?"),
                isPresented: $isConfirmingLeave,
                titleVisibility: .visible
            ) {
                Button("Leave Household", role: .destructive) { perform { try await households.leaveHousehold() } }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("You’ll lose access to its plans and lists, and you’ll need a new invitation to rejoin.")
            }
        } footer: {
            if households.isOnlyAdmin == true {
                Text("You’re this household’s only admin. Make another member an admin before you leave.")
            }
        }
    }

    /// Whether reminders can reach this phone, with the one action that changes it.
    @ViewBuilder
    private func notificationsRow(_ push: PushNotificationStore) -> some View {
        switch push.authorization {
        case .authorized:
            LabeledContent("Notifications", value: String(localized: "On"))
        case .notDetermined:
            Button {
                Task { await push.requestAuthorizationIfNeeded() }
            } label: {
                DSIconRow(systemImage: "bell.badge") { Text("Turn On Notifications") }
            }
            .accessibilityHint("Lets order-day and low-stock reminders reach this phone.")
        case .denied:
            Button {
                if let url = URL(string: UIApplication.openNotificationSettingsURLString) { openURL(url) }
            } label: {
                DSIconRow(systemImage: "bell.slash") { Text("Turn On Notifications in Settings") }
            }
            .accessibilityHint("Notifications are off for DinnerOS on this phone.")
        }
    }

    @ViewBuilder
    private var accountSection: some View {
        let user = me?.user ?? session.currentUser
        Section {
            if let user {
                LabeledContent("Name") {
                    Text(user.displayName.isEmpty ? String(localized: "Not provided") : user.displayName)
                }
                LabeledContent("Email") {
                    Text(user.primaryEmail ?? String(localized: "Not shared"))
                }
            }
            if let push {
                notificationsRow(push)
            }
            if let biometricLock, let kind = biometricLock.kind {
                biometricRow(biometricLock, kind: kind)
            }
            if let me, !me.identities.isEmpty {
                LabeledContent("Signed In With") {
                    Text(me.identities.map { Self.providerName($0.provider) }.formatted(.list(type: .and)))
                }
            }
            if isLoadingAccount && me == nil {
                HStack {
                    ProgressView()
                    Text("Loading account…")
                        .foregroundStyle(DS.Colors.secondaryText)
                }
                .accessibilityElement(children: .combine)
            }
            if let accountError {
                VStack(alignment: .leading, spacing: DS.Space.s) {
                    Label(accountError, systemImage: "exclamationmark.triangle.fill")
                        .symbolRenderingMode(.multicolor)
                    Button("Try Again") {
                        Task { await loadAccount() }
                    }
                }
            }
        }
        Section {
            FeedbackLink()
            ContactUsLink()
            PrivacyPolicyLink()
        }
    }

    // MARK: - Sheets and confirmations

    @ViewBuilder
    private func sheetContent(_ sheet: Sheet) -> some View {
        switch sheet {
        case .invite:
            InviteMemberSheet(
                roles: households.access?.invitableRoles ?? [],
                householdName: households.current?.household.name ?? "")
        case .createHousehold:
            NavigationStack {
                CreateHouseholdForm(onCreated: { self.sheet = nil })
                    .toolbar { cancelButton }
            }
        case .joinHousehold:
            NavigationStack {
                JoinHouseholdForm(onJoined: { _ in self.sheet = nil })
                    .toolbar { cancelButton }
            }
        }
    }

    private var cancelButton: some ToolbarContent {
        ToolbarItem(placement: .cancellationAction) {
            Button("Cancel") { sheet = nil }
        }
    }

    private var confirmations: HouseholdConfirmations {
        HouseholdConfirmations(
            memberPendingRemoval: $memberPendingRemoval,
            pendingRoleChange: Binding(
                get: { pendingRoleChange.map { ($0.member, $0.role) } },
                set: { pendingRoleChange = $0.map { RoleChange(member: $0.0, role: $0.1) } }),
            invitationPendingRevoke: $invitationPendingRevoke,
            removeMember: { member in perform { try await households.removeMember(member) } },
            changeRole: { member, role in perform { try await households.changeRole(of: member, to: role) } },
            revokeInvitation: { invitation in perform { try await households.revokeInvitation(invitation) } })
    }

    // MARK: - Actions

    private func perform(_ action: @escaping @MainActor () async throws -> Void) {
        Task {
            isWorking = true
            defer { isWorking = false }
            do {
                try await action()
            } catch is CancellationError {
                // Nothing to report.
            } catch {
                // After a sign-out the root view replaces this screen, so no error is shown.
                guard session.currentUser != nil else { return }
                actionError = HouseholdStore.message(for: error)
            }
        }
    }

    private func loadAccount() async {
        guard !isLoadingAccount else { return }
        isLoadingAccount = true
        defer { isLoadingAccount = false }
        do {
            me = try await session.loadCurrentUser()
            accountError = nil
        } catch is CancellationError {
            // The view went away; nothing to show.
        } catch {
            guard session.currentUser != nil else { return }
            accountError = HouseholdStore.message(for: error)
        }
    }

    private static func providerName(_ provider: String) -> String {
        switch provider {
        case "apple": "Apple"
        case "google": "Google"
        case "dev": String(localized: "Developer")
        default: provider
        }
    }
}

/// One member, with a menu of the actions the current user may take.
private struct HouseholdMemberRow: View {
    let member: HouseholdMember
    let isCurrentUser: Bool
    let assignableRoles: [HouseholdRole]
    let canRemove: Bool
    let onChangeRole: (HouseholdRole) -> Void
    let onRemove: () -> Void

    var body: some View {
        HStack(spacing: DS.Space.m) {
            Image(systemName: member.role == .admin ? "person.crop.circle.badge.checkmark" : "person.crop.circle")
                .font(DS.Typography.title)
                .foregroundStyle(DS.Colors.accent)
                .dsIconColumn()
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: DS.Space.hairline) {
                Text(isCurrentUser ? "\(member.name) (You)" : member.name)
                Text(member.role.displayName)
                    .font(DS.Typography.subheadline)
                    .foregroundStyle(DS.Colors.secondaryText)
            }
            .accessibilityElement(children: .combine)
            Spacer()
            if !assignableRoles.isEmpty || canRemove {
                Menu {
                    ForEach(assignableRoles) { role in
                        Button("Make \(role.displayName)", systemImage: "person.badge.key") {
                            onChangeRole(role)
                        }
                    }
                    if canRemove {
                        Button("Remove from Household", systemImage: "person.badge.minus", role: .destructive) {
                            onRemove()
                        }
                    }
                } label: {
                    Image(systemName: "ellipsis.circle")
                        .imageScale(.large)
                        .accessibilityLabel("Manage \(member.name)")
                }
            }
        }
    }
}

/// The Household tab's confirmation dialogs, kept out of the main view body.
private struct HouseholdConfirmations: ViewModifier {
    @Binding var memberPendingRemoval: HouseholdMember?
    @Binding var pendingRoleChange: (HouseholdMember, HouseholdRole)?
    @Binding var invitationPendingRevoke: HouseholdInvitation?
    let removeMember: (HouseholdMember) -> Void
    let changeRole: (HouseholdMember, HouseholdRole) -> Void
    let revokeInvitation: (HouseholdInvitation) -> Void

    func body(content: Content) -> some View {
        content
            .confirmationDialog(
                Text("Remove \(memberPendingRemoval?.name ?? "")?"),
                isPresented: Binding(presenting: $memberPendingRemoval),
                titleVisibility: .visible,
                presenting: memberPendingRemoval
            ) { member in
                Button("Remove", role: .destructive) { removeMember(member) }
                Button("Cancel", role: .cancel) {}
            } message: { _ in
                Text("They’ll lose access to this household’s plans and lists.")
            }
            .confirmationDialog(
                roleChangeTitle,
                isPresented: Binding(
                    get: { pendingRoleChange != nil },
                    set: { if !$0 { pendingRoleChange = nil } }),
                titleVisibility: .visible,
                presenting: pendingRoleChange
            ) { change in
                Button("Make \(change.1.displayName)") { changeRole(change.0, change.1) }
                Button("Cancel", role: .cancel) {}
            } message: { change in
                Text(change.1.summary)
            }
            .confirmationDialog(
                Text("Revoke the invitation for \(invitationPendingRevoke?.email ?? "")?"),
                isPresented: Binding(presenting: $invitationPendingRevoke),
                titleVisibility: .visible,
                presenting: invitationPendingRevoke
            ) { invitation in
                Button("Revoke Invitation", role: .destructive) { revokeInvitation(invitation) }
                Button("Cancel", role: .cancel) {}
            } message: { _ in
                Text("Its link and code will stop working.")
            }
    }

    private var roleChangeTitle: Text {
        guard let (member, role) = pendingRoleChange else { return Text(verbatim: "") }
        return Text("Make \(member.name) \(role.displayName.lowercased())?")
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        HouseholdView()
    }
    .environment(session)
    .environment(HouseholdPreviewData.store(session: session))
    .environment(PlusStore(session: session, api: nil, purchaser: StoreKitPlusPurchaser()))
    .environment(RecipePreviewData.library(session: session))
    .environment(AutopilotPreviewData.store(session: session))
}

/// The top of the Household tab: the household's name, its members' initials, and its Plus.
/// The name is a title one step under a large title, below the tab's small "Household" header,
/// so the tab doesn't show two large titles (build 121).
private struct HouseholdHeaderCard: View {
    let detail: HouseholdDetail
    let plusLine: String?
    @ScaledMetric(relativeTo: .subheadline) private var avatarSize: CGFloat = 36

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Space.s) {
            // One step below a title, so it reads as the card's heading, not a second title
            // under the tab's "Household" (build 121, review 2).
            Text(detail.household.name)
                .font(DS.Typography.sectionTitle)
                .accessibilityAddTraits(.isHeader)
            if let members = detail.members, !members.isEmpty {
                HStack(spacing: -DS.Space.s) {
                    ForEach(members.prefix(6)) { member in
                        Text(Self.initials(member.name))
                            .font(DS.Typography.subheadline.bold())
                            // Readable on the accent fill in light and dark mode.
                            .foregroundStyle(DS.Colors.onAccent)
                            .frame(width: avatarSize, height: avatarSize)
                            .background(DS.Colors.accent, in: .circle)
                            .overlay(Circle().stroke(DS.Colors.groupedBackground, lineWidth: DS.Space.hairline))
                    }
                    if members.count > 6 {
                        Text("+\(members.count - 6)")
                            .font(DS.Typography.footnote.bold())
                            .padding(.leading, DS.Space.ml)
                    }
                }
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(members.map(\.name).formatted(.list(type: .and)))
            }
            if let plusLine {
                Label(plusLine, systemImage: "sparkles")
                    .font(DS.Typography.subheadline)
                    .foregroundStyle(DS.Colors.secondaryText)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        // Clear of the row's rounded corners, which would otherwise clip the first letter.
        .padding(.vertical, DS.Space.s)
    }

    static func initials(_ name: String) -> String {
        let letters = name.split(separator: " ").prefix(2).compactMap(\.first)
        return letters.isEmpty ? "?" : String(letters).uppercased()
    }
}

extension View {
    /// No list spacing above this section, so the first card sits right under the bar's title
    /// (build 122, Hark's review item 16: the gap was too large on the Duo). iOS 26 and later.
    @ViewBuilder
    fileprivate func noTopSectionMargin() -> some View {
        if #available(iOS 26.0, *) {
            listSectionMargins(.top, 0)
        } else {
            self
        }
    }
}
