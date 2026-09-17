import SwiftUI

/// The shown week's item-by-item cost, and savings against the meal kit over recent weeks.
struct SavingsView: View {
    @Environment(ShoppingStore.self) private var shopping

    var body: some View {
        List {
            if let cost = shopping.weekCost {
                Section {
                    WeekCostSummary(cost: cost)
                } header: {
                    Text(ISOWeek(cost.week)?.rangeLabel(weekStartsOn: shopping.weekStartsOn) ?? cost.week)
                } footer: {
                    if let kit = cost.mealKit {
                        Text("Meal kit: \(WeekCostText.mealKit(kit)).")
                    }
                }
                if !cost.items.isEmpty {
                    Section {
                        ForEach(cost.items) { item in
                            WeekCostItemRow(item: item)
                        }
                    } header: {
                        Text("Items")
                    } footer: {
                        Text(
                            "Used this week counts what this week's meals needed. The rest stays in your pantry and counts toward the weeks you use it."
                        )
                    }
                }
            }
            recentWeeks
        }
        .navigationTitle("Savings")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            await shopping.loadSavings()
        }
        .refreshable {
            await shopping.loadWeekCost()
            await shopping.loadSavings()
        }
    }

    @ViewBuilder
    private var recentWeeks: some View {
        switch shopping.savingsPhase {
        case .idle, .loading:
            if shopping.savings == nil {
                ProgressView()
                    .frame(maxWidth: .infinity)
            }
        case .failed(let message):
            Section {
                FormErrorLabel(message: message)
                Button("Try Again") {
                    Task { await shopping.loadSavings() }
                }
            }
        case .loaded:
            EmptyView()
        }
        if let savings = shopping.savings, !savings.weeks.isEmpty {
            Section {
                ForEach(savings.weeks) { week in
                    SavingsWeekRow(
                        week: week, comparesMealKit: savings.mealKit != nil, weekStartsOn: shopping.weekStartsOn)
                }
            } header: {
                Text("Recent Weeks")
            } footer: {
                if let total = savings.totalSavedCents, savings.weeksCounted > 0 {
                    Text(totalText(total, weeks: savings.weeksCounted))
                }
            }
        }
    }

    private func totalText(_ cents: Int, weeks: Int) -> String {
        let amount = MoneyText.format(abs(cents))
        if cents >= 0 {
            return weeks == 1
                ? String(localized: "Saved \(amount) vs meal kit over 1 week.")
                : String(localized: "Saved \(amount) vs meal kit over \(weeks) weeks.")
        }
        return weeks == 1
            ? String(localized: "\(amount) more than meal kit over 1 week.")
            : String(localized: "\(amount) more than meal kit over \(weeks) weeks.")
    }
}

private struct WeekCostItemRow: View {
    let item: WeekCostItem

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(item.name)
                Text(detail)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            Text(item.priceCents.map { MoneyText.format($0) } ?? String(localized: "No price"))
                .monospacedDigit()
                .foregroundStyle(item.priceCents == nil ? AnyShapeStyle(.secondary) : AnyShapeStyle(Color.primary))
        }
        .accessibilityElement(children: .combine)
    }

    private var detail: String {
        if item.pantry == .notTracked {
            return String(localized: "Used this week")
        }
        switch (item.usedCents, item.stockedCents) {
        case (let used?, let stocked?):
            return String(
                localized: "In pantry · Used \(MoneyText.format(used)) · Stocked \(MoneyText.format(stocked))")
        case (let used?, nil):
            return String(localized: "In pantry · Used \(MoneyText.format(used))")
        default:
            return String(localized: "In pantry")
        }
    }
}

private struct SavingsWeekRow: View {
    let week: SavingsWeek
    let comparesMealKit: Bool
    let weekStartsOn: PlanDay

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(ISOWeek(week.week)?.rangeLabel(weekStartsOn: weekStartsOn) ?? week.week)
                Spacer(minLength: 8)
                Text(WeekCostText.amount(week.spentCents))
                    .monospacedDigit()
            }
            if let perMeal = week.costPerMealCents {
                Text("\(MoneyText.format(perMeal)) per meal")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if comparesMealKit, let saved = week.savedCents {
                Text(WeekCostText.comparison(savedCents: saved))
                    .font(.footnote)
                    .foregroundStyle(saved >= 0 ? AnyShapeStyle(Color.green) : AnyShapeStyle(Color.orange))
            }
        }
        .accessibilityElement(children: .combine)
    }
}
