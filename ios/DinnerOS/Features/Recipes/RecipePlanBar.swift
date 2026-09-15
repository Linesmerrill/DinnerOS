import SwiftUI

/// The recipe screen's bottom bar: "Add to Week" until the recipe is in the shown week, then a
/// stepper — "− 1 in your week (2 servings) +" — over the same `PlanStore` the cards use.
///
/// Plus moves to the recipe's next serving size up, minus to the next one down, and minus at
/// the smallest size removes the meal with an undo toast. When the same recipe is planned more
/// than once, the count says so and the stepper acts on the most recently added meal.
struct RecipePlanBar: View {
    let summary: RecipeSummary
    let recipe: Recipe?
    let entries: [PlanEntry]
    @Binding var servings: Int?
    let selections: [PlanCustomizationRequest.Selection]

    @Environment(PlanStore.self) private var plans
    @Environment(MenuStore.self) private var menu
    @Environment(MealPlanner.self) private var planner
    @Environment(HouseholdStore.self) private var households
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    @State private var isChoosingWeek = false

    private var entry: PlanEntry? { entries.last }

    private var options: [Int] { recipe?.servingOptions ?? [] }

    private var canEdit: Bool {
        households.access?.can(.planEdit) == true
    }

    /// A past week can't be planned; the sheet offers the weeks that can.
    private var isPastWeek: Bool {
        menu.selectedTiming == .past
    }

    private var isBusy: Bool {
        planner.busyRecipeIDs.contains(summary.id) || entry.map { planner.busyEntryIDs.contains($0.id) } == true
    }

    var body: some View {
        if canEdit {
            VStack(alignment: .leading, spacing: 8) {
                if let entry {
                    plannedBar(entry)
                } else {
                    addBar
                }
                if !plans.isDraft, !isPastWeek {
                    Text("This week is finalized. Reopen it to change the meals.")
                        .font(.caption)
                        .foregroundStyle(Color.secondary)
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.bar)
            .overlay(alignment: .top) { Divider() }
            .sheet(isPresented: $isChoosingWeek) {
                AddEntrySheet(recipeID: summary.id, recipeName: summary.name, fixedWeek: nil)
            }
        }
    }

    // MARK: Not planned

    private var addBar: some View {
        let layout =
            dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8))
            : AnyLayout(HStackLayout(alignment: .center, spacing: 12))
        return layout {
            if options.count > 1, let selected = servings {
                Menu {
                    Picker("Servings", selection: Binding(get: { selected }, set: { servings = $0 })) {
                        ForEach(options, id: \.self) { option in
                            Text(MenuFormat.servings(option)).tag(option)
                        }
                    }
                } label: {
                    HStack(spacing: 4) {
                        Text(MenuFormat.servings(selected))
                        Image(systemName: "chevron.up.chevron.down")
                            .font(.caption2)
                    }
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(Color.primary)
                }
                .accessibilityLabel("Servings")
            }
            if !dynamicTypeSize.isAccessibilitySize {
                Spacer(minLength: 0)
            }
            Button {
                add()
            } label: {
                if isBusy {
                    ProgressView()
                        .frame(maxWidth: dynamicTypeSize.isAccessibilitySize ? .infinity : 160)
                } else {
                    Text(isPastWeek ? "Add to Week…" : "Add to Week")
                        .fontWeight(.semibold)
                        .frame(maxWidth: dynamicTypeSize.isAccessibilitySize ? .infinity : 160)
                }
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .disabled(isBusy || (!plans.isDraft && !isPastWeek) || options.isEmpty)
            .contextMenu {
                Button("Add to Another Week…", systemImage: "calendar") {
                    isChoosingWeek = true
                }
            }
        }
    }

    // MARK: Planned

    private func plannedBar(_ entry: PlanEntry) -> some View {
        let layout =
            dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 10))
            : AnyLayout(HStackLayout(alignment: .center, spacing: 12))
        return layout {
            Menu {
                Section(entry.day?.title(in: plans.week) ?? String(localized: "Unscheduled")) {
                    ForEach(PlanDay.allCases) { day in
                        Button(day.name()) {
                            Task { await planner.move(entry, to: day) }
                        }
                        .disabled(entry.day == day)
                    }
                    Button("Unscheduled") {
                        Task { await planner.move(entry, to: nil) }
                    }
                    .disabled(entry.day == nil)
                }
                Button("Remove from Week", systemImage: "trash", role: .destructive) {
                    Task { await planner.remove(entry) }
                }
            } label: {
                HStack(spacing: 4) {
                    Image(systemName: "calendar")
                    Text(entry.day?.name() ?? String(localized: "Unscheduled"))
                    Image(systemName: "chevron.up.chevron.down")
                        .font(.caption2)
                }
                .font(.subheadline.weight(.medium))
                .foregroundStyle(Color.primary)
                .lineLimit(1)
            }
            .disabled(!plans.isDraft)
            .accessibilityLabel("Day")
            ServingsStepper(
                label: MenuFormat.planCount(entries.count, servings: entry.servings), style: .bar, isBusy: isBusy,
                canIncrease: ServingSizes.step(from: entry.servings, options: options, by: 1) != nil,
                decrease: { change(entry, by: -1) },
                increase: { change(entry, by: 1) }
            )
            .frame(maxWidth: dynamicTypeSize.isAccessibilitySize ? .infinity : 260)
            .disabled(!plans.isDraft)
        }
    }

    private func add() {
        guard !isPastWeek else {
            isChoosingWeek = true
            return
        }
        Task {
            await planner.add(
                recipeID: summary.id, name: summary.name, to: plans.week, servings: servings,
                selections: selections)
        }
    }

    private func change(_ entry: PlanEntry, by delta: Int) {
        Task {
            await planner.changeServings(entry, by: delta)
            if let updated = planner.entries(recipeID: summary.id).last {
                servings = updated.servings
            }
        }
    }
}

#Preview("Not in the week") {
    NavigationStack {
        Color(.systemBackground)
            .safeAreaInset(edge: .bottom, spacing: 0) {
                RecipePlanBar(
                    summary: MenuPreviewData.cards[1].recipe, recipe: RecipePreviewData.recipe, entries: [],
                    servings: .constant(2), selections: [])
            }
    }
    .menuPreviewEnvironment(plan: PlanPreviewData.emptyPlan)
}

#Preview("In the week") {
    NavigationStack {
        Color(.systemBackground)
            .safeAreaInset(edge: .bottom, spacing: 0) {
                RecipePlanBar(
                    summary: MenuPreviewData.cards[0].recipe, recipe: RecipePreviewData.recipe,
                    entries: Array(PlanPreviewData.plan.entries.prefix(1)), servings: .constant(4), selections: [])
            }
    }
    .menuPreviewEnvironment()
}
