import Foundation
import Observation
import os

/// The current household's Autopilot: its taste profile and choices, and the shown week's
/// context and proposal.
///
/// Main-actor state that lives as long as the app, like `PlanStore`. A proposal stays apart
/// from the plan until it's accepted; accepting hands the returned plan to `planDidChange`.
/// Every swap, accept, and dismiss sends the proposal's `version`. When another member
/// changed the proposal first (`409 proposal_changed` or `proposal_not_pending`, or `404`),
/// the store reloads it and sets `notice` instead of failing.
@Observable
final class AutopilotStore {
    enum Phase: Equatable {
        case idle
        case loading
        case loaded
        /// The profile or vocabulary failed to load, so there's nothing to show.
        case failed(String)
    }

    private(set) var householdID: String?
    /// The profile and vocabulary.
    private(set) var phase: Phase = .idle
    private(set) var profile: AutopilotProfile?
    private(set) var vocabulary: AutopilotVocabulary?

    /// The week `proposal` and `context` belong to.
    private(set) var week: ISOWeek?
    /// The week's latest proposal, whatever its status; `nil` when it has none.
    private(set) var proposal: AutopilotProposal?
    private(set) var context: AutopilotWeekContext?
    private(set) var isLoadingWeek = false
    /// Set when the week's proposal or context failed to load.
    private(set) var weekError: String?
    /// Slots the member switched off, sent as `excludeSlotIds` when accepting.
    private(set) var excludedSlotIDs: Set<String> = []
    private(set) var swappingSlotIDs: Set<String> = []
    private(set) var isGenerating = false
    /// Changes in flight.
    private(set) var pendingChanges = 0
    /// A short explanation to show once, such as why the suggestions just reloaded.
    var notice: String?

    var isSaving: Bool { pendingChanges > 0 }
    var isConfigured: Bool { profile?.configured == true }
    var limits: AutopilotLimits { vocabulary?.limits ?? .defaults }

    /// The shown week's proposal while it can still be swapped, accepted, or dismissed.
    var pendingProposal: AutopilotProposal? {
        guard let proposal, proposal.isPending else { return nil }
        return proposal
    }

    /// The slots accepting would add.
    var includedSlots: [AutopilotSlot] {
        pendingProposal?.slots.filter { !excludedSlotIDs.contains($0.id) } ?? []
    }

    /// Receives the plan an accepted proposal returns.
    @ObservationIgnored var planDidChange: ((Plan) -> Void)?

    @ObservationIgnored private let session: AuthSession
    @ObservationIgnored private let api: AutopilotAPI?
    @ObservationIgnored private let prompts: any AutopilotPromptStorage
    /// Incremented whenever the shown state is replaced, so a slow response for an old
    /// household or week can't overwrite newer state.
    @ObservationIgnored private var profileGeneration = 0
    @ObservationIgnored private var proposalGeneration = 0
    @ObservationIgnored private var contextGeneration = 0

    private static let logger = Logger(subsystem: "DinnerOS", category: "autopilot")

    init(session: AuthSession, api: AutopilotAPI?, prompts: any AutopilotPromptStorage) {
        self.session = session
        self.api = api
        self.prompts = prompts
    }

    /// A store frozen with the given state, for SwiftUI previews. It has no network access.
    static func preview(
        session: AuthSession, profile: AutopilotProfile?, vocabulary: AutopilotVocabulary?,
        proposal: AutopilotProposal? = nil, context: AutopilotWeekContext? = nil
    ) -> AutopilotStore {
        let store = AutopilotStore(session: session, api: nil, prompts: InMemoryAutopilotPrompts())
        store.householdID = profile?.householdID ?? "household-preview"
        store.profile = profile
        store.vocabulary = vocabulary
        store.phase = profile == nil ? .idle : .loaded
        store.proposal = proposal
        store.context = context
        store.week = (proposal?.week ?? context?.week).flatMap { ISOWeek($0) }
        return store
    }

    // MARK: - Profile

    /// Shows `householdID`'s Autopilot. Loads the profile and vocabulary unless they're
    /// already loaded or loading for that household.
    func activate(householdID: String) async {
        let switched = prepare(householdID: householdID)
        guard switched || phase == .idle || (phase != .loading && profile == nil) else { return }
        await loadProfile()
    }

    func reloadProfile() async {
        await loadProfile()
    }

    private func loadProfile() async {
        guard let api, let householdID else { return }
        profileGeneration += 1
        let started = profileGeneration
        if profile == nil || vocabulary == nil {
            phase = .loading
        }
        do {
            async let loadedProfile = session.authorized { token in
                try await api.profile(householdID: householdID, accessToken: token)
            }
            async let loadedVocabulary = session.authorized { token in
                try await api.vocabulary(householdID: householdID, accessToken: token)
            }
            let (profile, vocabulary) = try await (loadedProfile, loadedVocabulary)
            guard started == profileGeneration else { return }
            self.profile = profile
            self.vocabulary = vocabulary
            phase = .loaded
        } catch is CancellationError {
            if started == profileGeneration, phase == .loading { phase = .idle }
        } catch {
            guard started == profileGeneration else { return }
            Self.logger.notice("Autopilot profile load failed: \(Self.describe(error), privacy: .public)")
            if phase != .loaded {
                phase = .failed(HouseholdStore.message(for: error))
            }
        }
    }

    /// Whether to offer onboarding without being asked: the profile isn't configured, and
    /// this user hasn't been offered it for this household on this device.
    var shouldOfferOnboarding: Bool {
        guard
            let profile, !profile.configured, let householdID, let userID = session.currentUser?.id
        else { return false }
        return !prompts.hasPromptedOnboarding(userID: userID, householdID: householdID)
    }

    /// Records that onboarding was offered, so it isn't offered again automatically.
    func markOnboardingOffered() {
        guard let householdID, let userID = session.currentUser?.id else { return }
        prompts.setPromptedOnboarding(userID: userID, householdID: householdID)
    }

    /// Saves every section at once (onboarding).
    func saveProfile(_ settings: AutopilotSettings) async throws {
        let saved = try await mutate { api, householdID, token in
            try await api.replaceProfile(householdID: householdID, settings: settings, accessToken: token)
        }
        applyProfile(saved)
        Self.logger.info("Autopilot profile saved")
    }

    /// Saves `sections` from `settings`, each whole. Weekday rules can only use equipment
    /// the household has, so saving equipment also saves rules that changed with it.
    func saveSections(_ sections: Set<AutopilotSection>, from settings: AutopilotSettings) async throws {
        var sections = sections
        if sections.contains(.equipment), let profile, profile.weekdayRules != settings.weekdayRules {
            sections.insert(.weekdayRules)
        }
        guard !sections.isEmpty else { return }
        let update = AutopilotProfileUpdate(settings: settings, sections: sections)
        let saved = try await mutate { api, householdID, token in
            try await api.updateProfile(householdID: householdID, update: update, accessToken: token)
        }
        applyProfile(saved)
        Self.logger.info("Autopilot sections saved: \(sections.map(\.rawValue).sorted(), privacy: .public)")
    }

    private func applyProfile(_ saved: AutopilotProfile) {
        guard saved.householdID == householdID else { return }
        profileGeneration += 1
        profile = saved
        if vocabulary != nil {
            phase = .loaded
        }
    }

    /// Preference changes, newest first. Not stored.
    func history(limit: Int = 50) async throws -> [AutopilotHistoryItem] {
        guard let api, let householdID else { throw AuthSessionError.notConfigured }
        return try await session.authorized { token in
            try await api.history(householdID: householdID, limit: limit, accessToken: token)
        }
    }

    // MARK: - Recipe attributes

    /// What Autopilot derives from a recipe. Not stored.
    func attributes(recipeID: String) async throws -> AutopilotRecipeAttributes {
        guard let api, let householdID else { throw AuthSessionError.notConfigured }
        return try await session.authorized { token in
            try await api.attributes(householdID: householdID, recipeID: recipeID, accessToken: token)
        }
    }

    /// Overrides whether a recipe suits `method`, or returns it to automatic.
    func setMethod(
        _ method: String, to setting: AutopilotMethodSetting, recipeID: String
    ) async throws -> AutopilotRecipeAttributes {
        let methods: [String: Bool?] = [method: setting.overrideValue]
        return try await mutate { api, householdID, token in
            try await api.setOverride(
                householdID: householdID, recipeID: recipeID, methods: methods, accessToken: token)
        }
    }

    // MARK: - Week

    /// Shows `week`'s context and proposal for `householdID`, loading them.
    func showWeek(_ newWeek: ISOWeek, householdID: String) async {
        prepare(householdID: householdID)
        if newWeek != week {
            resetWeek(to: newWeek)
        }
        await loadWeek()
    }

    /// Pull to refresh.
    func reloadWeek() async {
        await loadWeek()
    }

    private func loadWeek() async {
        guard let week, api != nil else { return }
        isLoadingWeek = true
        async let proposal: Void = loadProposal(week: week)
        async let context: Void = loadContext(week: week)
        _ = await (proposal, context)
        if week == self.week {
            isLoadingWeek = false
        }
    }

    private func loadProposal(week target: ISOWeek) async {
        guard let api, let householdID else { return }
        proposalGeneration += 1
        let started = proposalGeneration
        do {
            let loaded = try await session.authorized { token in
                try await api.proposal(householdID: householdID, week: target, accessToken: token)
            }
            guard started == proposalGeneration else { return }
            apply(loaded)
        } catch let error as APIError where error.status == 404 {
            guard started == proposalGeneration, target == week else { return }
            proposal = nil
            excludedSlotIDs = []
        } catch is CancellationError {
            return
        } catch {
            guard started == proposalGeneration, target == week else { return }
            Self.logger.notice("Autopilot proposal load failed: \(Self.describe(error), privacy: .public)")
            weekError = HouseholdStore.message(for: error)
        }
    }

    private func loadContext(week target: ISOWeek) async {
        _ = try? await refreshContext(week: target)
    }

    /// Loads a week's context for editing, and shows it when it's the shown week.
    @discardableResult
    func refreshContext(week target: ISOWeek) async throws -> AutopilotWeekContext {
        guard let api, let householdID else { throw AuthSessionError.notConfigured }
        contextGeneration += 1
        let started = contextGeneration
        do {
            let loaded = try await session.authorized { token in
                try await api.weekContext(householdID: householdID, week: target, accessToken: token)
            }
            if started == contextGeneration, target == week, loaded.householdID == self.householdID {
                context = loaded
            }
            return loaded
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            if started == contextGeneration, target == week {
                Self.logger.notice("Autopilot context load failed: \(Self.describe(error), privacy: .public)")
                weekError = HouseholdStore.message(for: error)
            }
            throw error
        }
    }

    /// Replaces the week's context.
    func saveContext(_ draft: AutopilotWeekContextDraft, week target: ISOWeek) async throws {
        let saved = try await mutate { api, householdID, token in
            try await api.saveWeekContext(householdID: householdID, week: target, draft: draft, accessToken: token)
        }
        if target == week, saved.householdID == householdID {
            contextGeneration += 1
            context = saved
        }
        Self.logger.info("Autopilot week context saved")
    }

    /// Clears the week's context.
    func clearContext(week target: ISOWeek) async throws {
        try await mutate { api, householdID, token in
            try await api.clearWeekContext(householdID: householdID, week: target, accessToken: token)
        }
        if target == week {
            await loadContext(week: target)
        }
        Self.logger.info("Autopilot week context cleared")
    }

    // MARK: - Proposals

    /// Generates a proposal for `week`, replacing any earlier one, and shows that week.
    func generate(week target: ISOWeek) async throws {
        if target != week {
            resetWeek(to: target)
        }
        isGenerating = true
        defer { isGenerating = false }
        notice = nil
        let generated = try await mutate { api, householdID, token in
            try await api.generate(householdID: householdID, week: target, accessToken: token)
        }
        apply(generated)
        Self.logger.info("Autopilot week generated with \(generated.slots.count, privacy: .public) meals")
    }

    /// Switches a slot on or off for accepting.
    func setSlot(_ slotID: String, included: Bool) {
        guard pendingProposal?.slot(id: slotID) != nil else { return }
        if included {
            excludedSlotIDs.remove(slotID)
        } else {
            excludedSlotIDs.insert(slotID)
        }
    }

    /// Replaces a slot's meal with the next best one.
    func swap(slotID: String) async throws {
        guard pendingProposal?.slot(id: slotID) != nil, !swappingSlotIDs.contains(slotID) else { return }
        swappingSlotIDs.insert(slotID)
        defer { swappingSlotIDs.remove(slotID) }
        let updated = try await changeProposal { api, householdID, week, version, token in
            try await api.swap(
                householdID: householdID, week: week, slotID: slotID, version: version, accessToken: token)
        }
        if let updated {
            apply(updated)
        }
    }

    /// Adds the included slots to the plan. Returns `nil` when another member changed the
    /// proposal first (it reloads, and `notice` says so).
    @discardableResult
    func accept() async throws -> AutopilotAcceptResult? {
        guard let proposal = pendingProposal else { return nil }
        let excluded = proposal.slots.map(\.id).filter { excludedSlotIDs.contains($0) }
        let result = try await changeProposal { api, householdID, week, version, token in
            try await api.accept(
                householdID: householdID, week: week, version: version, excludeSlotIDs: excluded, accessToken: token)
        }
        guard let result else { return nil }
        apply(result.proposal)
        planDidChange?(result.plan)
        Self.logger.info(
            "Autopilot week accepted: \(result.added.count, privacy: .public) added, \(result.skipped.count, privacy: .public) skipped"
        )
        return result
    }

    /// Rejects the pending proposal; the plan doesn't change.
    func dismissProposal() async throws {
        let updated = try await changeProposal { api, householdID, week, version, token in
            try await api.reject(householdID: householdID, week: week, version: version, accessToken: token)
        }
        if let updated {
            apply(updated)
        }
    }

    /// Shows a proposal returned by the API when it's for the shown household and week.
    private func apply(_ updated: AutopilotProposal) {
        guard updated.householdID == householdID, updated.week == week?.description else { return }
        // Drops a load that started before this response.
        proposalGeneration += 1
        if updated.id == proposal?.id {
            excludedSlotIDs.formIntersection(updated.slots.map(\.id))
        } else {
            excludedSlotIDs = []
        }
        proposal = updated
        weekError = nil
    }

    // MARK: - Reset

    /// Forgets everything, for sign-out.
    func reset() {
        clear()
        householdID = nil
    }

    /// Clears state for another household. Returns whether it switched.
    @discardableResult
    private func prepare(householdID: String) -> Bool {
        guard householdID != self.householdID else { return false }
        clear()
        self.householdID = householdID
        return true
    }

    private func clear() {
        profileGeneration += 1
        phase = .idle
        profile = nil
        vocabulary = nil
        week = nil
        resetWeek(to: nil)
    }

    private func resetWeek(to newWeek: ISOWeek?) {
        proposalGeneration += 1
        contextGeneration += 1
        week = newWeek
        proposal = nil
        context = nil
        isLoadingWeek = false
        weekError = nil
        excludedSlotIDs = []
        swappingSlotIDs = []
        notice = nil
    }

    // MARK: - Helpers

    private func mutate<Result: Sendable>(
        _ operation: (AutopilotAPI, String, String) async throws -> Result
    ) async throws -> Result {
        guard let api, let householdID else { throw AuthSessionError.notConfigured }
        pendingChanges += 1
        defer { pendingChanges -= 1 }
        do {
            return try await session.authorized { token in try await operation(api, householdID, token) }
        } catch let error as APIError {
            Self.logger.notice("Autopilot change rejected: \(Self.describe(error), privacy: .public)")
            throw error
        }
    }

    /// Runs a change to the pending proposal with its current version. When the proposal
    /// changed underneath (`proposal_changed`, `proposal_not_pending`, or `404`), reloads
    /// it, sets `notice`, and returns `nil`. Other failures are thrown.
    private func changeProposal<Result: Sendable>(
        _ operation: (AutopilotAPI, String, ISOWeek, Int, String) async throws -> Result
    ) async throws -> Result? {
        guard let week, let version = pendingProposal?.version else { return nil }
        do {
            return try await mutate { api, householdID, token in
                try await operation(api, householdID, week, version, token)
            }
        } catch let error as APIError where Self.meansProposalChanged(error) {
            await loadProposal(week: week)
            if week == self.week {
                notice = HouseholdStore.message(for: error)
            }
            return nil
        }
    }

    static func meansProposalChanged(_ error: APIError) -> Bool {
        switch AutopilotConflict(error) {
        case .proposalChanged, .proposalNotPending: true
        default: error.status == 404
        }
    }

    /// Status and error code only; never tokens or response bodies.
    private static func describe(_ error: any Error) -> String {
        switch error as? APIError {
        case .server(let status, let code, _, let requestID):
            "\(status) \(code) request=\(requestID ?? "-")"
        case .transport(let code):
            "transport \(code.rawValue)"
        case .invalidResponse:
            "invalid response"
        case .decoding(let type):
            "decoding \(type)"
        case nil:
            String(describing: type(of: error))
        }
    }
}
