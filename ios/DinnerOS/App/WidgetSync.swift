import Foundation
import OSLog
import WidgetKit

/// Keeps the home screen widgets current: whenever a plan loads or changes, it writes this week
/// and next week to the shared snapshot and asks WidgetKit to redraw. Cheap to call often; the
/// work is debounced and skipped when nothing changed.
@MainActor
final class WidgetSync {
    private let plans: PlanStore
    private let library: RecipeLibrary
    private var pending: Task<Void, Never>?
    private var lastWritten: DinnerWidgetSnapshot?
    private static let logger = Logger(subsystem: "com.linesmerrill.dinneros", category: "widgets")

    init(plans: PlanStore, library: RecipeLibrary) {
        self.plans = plans
        self.library = library
    }

    /// Something changed; refresh shortly.
    func planChanged() {
        pending?.cancel()
        pending = Task { [weak self] in
            do { try await Task.sleep(for: .seconds(1)) } catch { return }
            await self?.refresh()
        }
    }

    /// Signed out or left the household: the widget asks to open the app.
    func clear() {
        pending?.cancel()
        lastWritten = nil
        DinnerWidgetStore.clear()
        WidgetCenter.shared.reloadAllTimelines()
    }

    func refresh() async {
        guard plans.householdID != nil else { return }
        let current = plans.currentWeek
        let zone = plans.planningTimeZone
        let first = plans.weekStartsOn
        do {
            let thisWeek = plans.plan?.week == current.description ? plans.plan : try await plans.fetch(week: current)
            let nextWeek = try await plans.fetch(week: current.adding(weeks: 1))
            guard let thisWeek else { return }
            let snapshot = makeSnapshot(this: thisWeek, next: nextWeek, week: current, first: first, zone: zone)
            guard
                snapshot.days != lastWritten?.days || snapshot.nextWeek != lastWritten?.nextWeek
                    || snapshot.unscheduled != lastWritten?.unscheduled
            else { return }
            DinnerWidgetStore.save(snapshot)
            lastWritten = snapshot
            WidgetCenter.shared.reloadAllTimelines()
        } catch is CancellationError {
            return
        } catch {
            Self.logger.notice("Widget refresh failed")
        }
    }

    private func makeSnapshot(
        this plan: Plan, next: Plan?, week: ISOWeek, first: PlanDay, zone: TimeZone
    ) -> DinnerWidgetSnapshot {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = zone
        let formatter = DateFormatter()
        formatter.calendar = calendar
        formatter.timeZone = zone
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "yyyy-MM-dd"
        let start = week.startDate(weekStartsOn: first) ?? .now
        let dates = (0..<7).compactMap { calendar.date(byAdding: .day, value: $0, to: start) }.map(formatter.string)
        let days = dates.map { date in
            DinnerWidgetSnapshot.Day(date: date, meals: plan.entries.filter { $0.date == date }.map(meal))
        }
        let nextMeals = (next?.entriesInDayOrder(weekStartsOn: first) ?? []).map(meal)
        return DinnerWidgetSnapshot(
            updatedAt: .now, days: days, unscheduled: plan.entries.filter { $0.date == nil }.map(meal),
            nextWeek: nextMeals, timeZone: zone.identifier)
    }

    private func meal(_ entry: PlanEntry) -> DinnerWidgetSnapshot.Meal {
        let summary = library.items.first { $0.id == entry.recipe.id }
        return DinnerWidgetSnapshot.Meal(
            recipeID: entry.recipe.id, name: entry.recipe.name, imageURL: entry.recipe.imageURLString,
            minutes: summary?.displayMinutes, isAddon: entry.recipe.isAddon)
    }
}
