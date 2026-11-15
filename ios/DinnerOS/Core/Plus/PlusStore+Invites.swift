import Foundation

/// Invite links and the free month checklist for a new household (build 112).
///
/// A link (or a code typed during setup) is saved on this device. Once the person accepts it
/// and makes a new household, the code is used for them and the checklist opens. A household
/// that already existed when the code was accepted never uses it by accident.
extension PlusStore {
    nonisolated static let savedInviteKey = "plus.savedInvite"

    static func referralCardHiddenKey(_ householdID: String) -> String { "plus.referralCardHidden.\(householdID)" }
    static func referralPendingKey(_ householdID: String) -> String { "plus.referralPending.\(householdID)" }
    static func freeMonthShownKey(_ householdID: String) -> String { "plus.freeMonthShown.\(householdID)" }

    static func loadSavedInvite(_ defaults: UserDefaults) -> SavedInvite? {
        guard let data = defaults.data(forKey: savedInviteKey) else { return nil }
        return try? JSONCoding.makeDecoder().decode(SavedInvite.self, from: data)
    }

    func saveInvite(_ invite: SavedInvite?) {
        savedInvite = invite
        if let invite, let data = try? JSONCoding.makeEncoder().encode(invite) {
            defaults.set(data, forKey: Self.savedInviteKey)
        } else {
            defaults.removeObject(forKey: Self.savedInviteKey)
        }
    }

    /// Who sent `code`, for the invite screen and setup. Needs no sign-in.
    func invitePreview(_ code: String) async throws -> InvitePreview {
        guard let api = inviteAPI else { throw AuthSessionError.notConfigured }
        return try await api.invitePreview(code: code)
    }

    /// Yes to an invite: kept until a new household is made, then used for it.
    func acceptInvite(code: String, title: String?, now: Date = .now) {
        saveInvite(SavedInvite(code: code, accepted: true, acceptedAt: now, title: title))
        Self.inviteLogger.info("Invite code accepted")
    }

    /// Not now: the screen closes, and setup can still offer the code.
    func declineInvite() {
        incomingInvite = nil
    }

    /// Forgets the saved code: it was for someone else's household, or it's been used.
    func clearSavedInvite() {
        saveInvite(nil)
    }

    /// Uses an accepted code for a household made after it was accepted, then opens the
    /// checklist. Called once the household's Plus has loaded.
    func useSavedInvite(householdCreatedAt: Date) async {
        guard let invite = savedInvite, invite.accepted, let acceptedAt = invite.acceptedAt else { return }
        // Made before the code was accepted: an existing household, which codes aren't for.
        // A minute of slack covers a clock that's a little off from the server's.
        guard householdCreatedAt >= acceptedAt.addingTimeInterval(-60) else {
            clearSavedInvite()
            return
        }
        guard status?.enabled == true else { return }
        // Already used one: nothing to do but show where it stands.
        if status?.referralProgress != nil {
            clearSavedInvite()
            return
        }
        do {
            try await redeem(code: invite.code)
            clearSavedInvite()
            checklistWaiting = true
        } catch is CancellationError {
        } catch {
            clearSavedInvite()
            inviteError = HouseholdStore.message(for: error)
        }
    }

    // MARK: The Menu card

    /// This household's checklist while it's still waiting on setup.
    var pendingReferral: ReferralProgress? {
        let progress = status?.referralProgress ?? referral?.referralProgress
        return progress?.isPending == true ? progress : nil
    }

    /// Whether the Menu shows the checklist card. Give a Free Month always shows it.
    var showsReferralCard: Bool {
        _ = hiddenCardsVersion
        guard pendingReferral != nil, let householdID = currentHouseholdID else { return false }
        return !defaults.bool(forKey: Self.referralCardHiddenKey(householdID))
    }

    /// Hide on the Menu card. Give a Free Month still has the checklist.
    func hideReferralCard() {
        guard let householdID = currentHouseholdID else { return }
        defaults.set(true, forKey: Self.referralCardHiddenKey(householdID))
        bumpHiddenCards()
    }

    // MARK: The free month confirmation

    /// Remembers a checklist seen waiting on this device, and shows the confirmation once when
    /// it's done. A household that finished before this device saw it waiting gets no surprise.
    func noteReferralProgress(_ progress: ReferralProgress?, householdID: String) {
        guard let progress else { return }
        switch progress.state {
        case "pending":
            defaults.set(true, forKey: Self.referralPendingKey(householdID))
        case "qualified":
            guard defaults.bool(forKey: Self.referralPendingKey(householdID)),
                !defaults.bool(forKey: Self.freeMonthShownKey(householdID)), freeMonthOn == nil
            else { return }
            freeMonthOn = progress
        default:
            defaults.removeObject(forKey: Self.referralPendingKey(householdID))
        }
    }

    /// The confirmation closed: don't show it again here.
    func freeMonthShown() {
        guard let householdID = currentHouseholdID else { return }
        defaults.set(true, forKey: Self.freeMonthShownKey(householdID))
    }
}

extension ReferralProgress: Identifiable {
    nonisolated var id: String { state + message }
}
