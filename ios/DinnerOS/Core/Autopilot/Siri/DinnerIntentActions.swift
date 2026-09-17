import Foundation
import os

/// The signed-in member's current household, as the Siri intents need it.
nonisolated struct IntentHousehold: Equatable, Sendable {
    let id: String
    let timeZone: TimeZone
    /// Whether the member may plan meals (`plan.edit`).
    let canPlan: Bool
    /// The day the household's weeks start on.
    var weekStartsOn: PlanDay = PlanDay.defaultWeekStart
}

/// Where the member stands before an intent can do anything.
nonisolated enum IntentAccount: Equatable, Sendable {
    case signedOut
    case noHousehold
    case unavailable(String)
    case ready(IntentHousehold)
}

/// What the intents need from the app. `LiveDinnerIntentServices` calls the existing stores;
/// tests use a fake.
@MainActor
protocol DinnerIntentServices: AnyObject {
    /// Restores the session and loads the households, waiting for a launch already doing so.
    func account() async -> IntentAccount
    func isAutopilotConfigured(householdID: String) async throws -> Bool
    func plan(week: ISOWeek, householdID: String) async throws -> Plan
    /// Generates with the same device context path the app uses (never prompting).
    func generate(week: ISOWeek, householdID: String) async throws -> AutopilotProposal
    /// Opens the suggestions for review once the app is in front.
    func openReview(week: ISOWeek)
}

/// Rephrases the deterministic summary more warmly, or returns `nil` to keep it.
@MainActor
protocol WeekSummaryPhraser: AnyObject {
    func rephrase(_ summary: String, meals: [String]) async -> String?
}

/// "Plan my dinners": what happened.
nonisolated enum PlanDinnersOutcome: Equatable, Sendable {
    case signedOut
    case noHousehold
    case unavailable(String)
    case notAllowed
    case notSetUp
    case finalized(PlanningWeek)
    case skipped(PlanningWeek)
    case nothingSuggested(PlanningWeek)
    /// Meals in the week's day order, as "Monday: Tacos".
    case planned(PlanningWeek, meals: [PlannedMeal])
}

/// A week key and the day the household's weeks start on, which together name its dates.
nonisolated struct PlanningWeek: Hashable, Sendable {
    let week: ISOWeek
    let weekStartsOn: PlanDay

    func rangeLabel(locale: Locale = .autoupdatingCurrent) -> String {
        week.rangeLabel(weekStartsOn: weekStartsOn, locale: locale)
    }
}

nonisolated struct PlannedMeal: Equatable, Sendable {
    let day: PlanDay
    let name: String
}

/// "What's for dinner tonight": what's planned.
nonisolated enum TonightOutcome: Equatable, Sendable {
    case signedOut
    case noHousehold
    case unavailable(String)
    case nothingPlanned
    case planned(mains: [String], addOns: [String])
}

/// The Siri intents' behavior, kept out of the `AppIntent` types so it can be tested.
@MainActor
final class DinnerIntentActions {
    private let services: any DinnerIntentServices
    private let phraser: (any WeekSummaryPhraser)?
    private let now: () -> Date

    private static let logger = Logger(subsystem: "DinnerOS", category: "siri")

    init(
        services: any DinnerIntentServices, phraser: (any WeekSummaryPhraser)? = nil,
        now: @escaping () -> Date = Date.init
    ) {
        self.services = services
        self.phraser = phraser
        self.now = now
    }

    /// The week "Plan my dinners" plans: this week for its first four days, while most of its
    /// dinners are still ahead (Monday through Thursday for Monday weeks, Sunday through
    /// Wednesday for Sunday weeks); next week after that, when households plan the week coming.
    static func upcomingWeek(now: Date, timeZone: TimeZone, weekStartsOn: PlanDay) -> ISOWeek {
        let current = ISOWeek.current(in: timeZone, weekStartsOn: weekStartsOn, now: now)
        let today = PlanDay.containing(now, in: timeZone)
        return today.position(inWeekStartingOn: weekStartsOn) >= 4 ? current.next : current
    }

    func planDinners() async -> PlanDinnersOutcome {
        let household: IntentHousehold
        switch await services.account() {
        case .signedOut: return .signedOut
        case .noHousehold: return .noHousehold
        case .unavailable(let message): return .unavailable(message)
        case .ready(let ready): household = ready
        }
        guard household.canPlan else { return .notAllowed }
        let week = Self.upcomingWeek(now: now(), timeZone: household.timeZone, weekStartsOn: household.weekStartsOn)
        let named = PlanningWeek(week: week, weekStartsOn: household.weekStartsOn)
        do {
            guard try await services.isAutopilotConfigured(householdID: household.id) else { return .notSetUp }
            if try await services.plan(week: week, householdID: household.id).status != .draft {
                return .finalized(named)
            }
            let proposal = try await services.generate(week: week, householdID: household.id)
            guard !proposal.slots.isEmpty else {
                let skipped = proposal.messages.contains { $0.code == "week_skipped" }
                return skipped ? .skipped(named) : .nothingSuggested(named)
            }
            services.openReview(week: week)
            let start = household.weekStartsOn
            let meals = proposal.slots
                .sorted { $0.day.position(inWeekStartingOn: start) < $1.day.position(inWeekStartingOn: start) }
                .map { PlannedMeal(day: $0.day, name: $0.recipe.name) }
            Self.logger.info("Siri planned \(meals.count, privacy: .public) meals")
            return .planned(named, meals: meals)
        } catch let error where AutopilotConflict(error) == .planFinalized {
            return .finalized(named)
        } catch {
            return .unavailable(HouseholdStore.message(for: error))
        }
    }

    func tonight() async -> TonightOutcome {
        let household: IntentHousehold
        switch await services.account() {
        case .signedOut: return .signedOut
        case .noHousehold: return .noHousehold
        case .unavailable(let message): return .unavailable(message)
        case .ready(let ready): household = ready
        }
        let date = now()
        let week = ISOWeek.current(in: household.timeZone, weekStartsOn: household.weekStartsOn, now: date)
        let today = PlanDay.containing(date, in: household.timeZone)
        do {
            let entries = try await services.plan(week: week, householdID: household.id).entries.filter {
                $0.day == today
            }
            let mains = entries.filter { !$0.recipe.isAddon }.map(\.recipe.name)
            let addOns = entries.filter(\.recipe.isAddon).map(\.recipe.name)
            // An add-on alone still answers the question.
            guard !(mains + addOns).isEmpty else { return .nothingPlanned }
            return .planned(mains: mains.isEmpty ? addOns : mains, addOns: mains.isEmpty ? [] : addOns)
        } catch {
            return .unavailable(HouseholdStore.message(for: error))
        }
    }

    /// The spoken and shown reply for "Plan my dinners". A planned week may be rephrased on
    /// the device; anything else is fixed text.
    func dialog(for outcome: PlanDinnersOutcome) async -> String {
        let text = DinnerIntentText.planDinners(outcome)
        guard case .planned(_, let meals) = outcome, let phraser else { return text }
        return await phraser.rephrase(text, meals: meals.map(\.name)) ?? text
    }
}

/// The intents' deterministic wording.
nonisolated enum DinnerIntentText {
    static let signedOut = String(localized: "You're signed out of DinnerOS. Open the app and sign in first.")
    static let noHousehold = String(
        localized: "You're not in a household yet. Open DinnerOS to create or join one first.")

    static func planDinners(_ outcome: PlanDinnersOutcome, locale: Locale = .autoupdatingCurrent) -> String {
        switch outcome {
        case .signedOut: return signedOut
        case .noHousehold: return noHousehold
        case .unavailable(let message):
            return String(localized: "DinnerOS couldn't plan your dinners right now. \(message)")
        case .notAllowed:
            return String(localized: "Only household members who can plan meals can run Autopilot.")
        case .notSetUp:
            return String(localized: "Autopilot isn't set up yet. Open DinnerOS and answer a few questions first.")
        case .finalized(let week):
            return String(
                localized:
                    "The week of \(week.rangeLabel(locale: locale)) is finalized. Reopen it in DinnerOS to plan again.")
        case .skipped(let week):
            return String(
                localized: "You're skipping the week of \(week.rangeLabel(locale: locale)), so there's nothing to plan."
            )
        case .nothingSuggested(let week):
            return String(
                localized:
                    "Autopilot couldn't find dinners for the week of \(week.rangeLabel(locale: locale)). Open DinnerOS to see why."
            )
        case .planned(let week, let meals):
            let range = week.rangeLabel(locale: locale)
            let first = meals.first.map { String(localized: "\($0.name) on \($0.day.name(locale: locale))") } ?? ""
            if meals.count == 1 {
                return String(localized: "Autopilot suggested \(first) for \(range). It's ready for you to review.")
            }
            return String(
                localized:
                    "Autopilot suggested \(meals.count) dinners for \(range), starting with \(first). They're ready for you to review."
            )
        }
    }

    static func tonight(_ outcome: TonightOutcome, locale: Locale = .autoupdatingCurrent) -> String {
        switch outcome {
        case .signedOut: return signedOut
        case .noHousehold: return noHousehold
        case .unavailable(let message):
            return String(localized: "DinnerOS couldn't check tonight's dinner right now. \(message)")
        case .nothingPlanned:
            return String(localized: "Nothing's planned for dinner tonight.")
        case .planned(let mains, let addOns):
            let meal = mains.formatted(.list(type: .and).locale(locale))
            guard !addOns.isEmpty else { return String(localized: "Tonight's dinner is \(meal).") }
            let extras = addOns.formatted(.list(type: .and).locale(locale))
            return String(localized: "Tonight's dinner is \(meal), with \(extras).")
        }
    }
}
