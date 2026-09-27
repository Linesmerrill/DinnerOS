import SwiftUI

/// The quiet place to review what the household isn't buying, reached from the grocery list.
struct SkippedIngredientsSheet: View {
    let week: ISOWeek

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            SkippedIngredientsView(week: week)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { dismiss() }
                    }
                }
        }
    }
}

/// Everything the household leaves out, split by how far each skip reaches, with one tap to
/// resume and one to change the lifetime.
///
/// The sections are the whole design: "back next week", "never again", and "not in this dish"
/// are different promises, and a single list would make the household read each row to tell
/// them apart. Every exclusion — from the grocery list, the Shop tab, or a recipe — is here.
struct SkippedIngredientsView: View {
    let week: ISOWeek

    @Environment(GrocerySkipStore.self) private var skips
    @Environment(HouseholdStore.self) private var households

    @State private var actionError: String?

    /// Hiding the actions is a convenience; the API enforces `plan.edit`.
    private var canEdit: Bool {
        households.access?.can(.planEdit) == true && !skips.isForbidden
    }

    var body: some View {
        content
            .navigationTitle("Left Out")
            .navigationBarTitleDisplayMode(.inline)
            .alert("Couldn't Change That", isPresented: Binding(presenting: $actionError)) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(actionError ?? "")
            }
    }

    @ViewBuilder
    private var content: some View {
        switch skips.phase {
        case .idle, .loading:
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Couldn't Load These", systemImage: "exclamationmark.triangle")
            } description: {
                Text(message)
            } actions: {
                Button("Try Again") {
                    Task { await skips.retry() }
                }
                .buttonStyle(.borderedProminent)
            }
        case .loaded:
            list
        }
    }

    private var list: some View {
        List {
            if let refreshError = skips.refreshError {
                FormErrorLabel(message: refreshError)
            }
            if skips.items.isEmpty {
                ContentUnavailableView(
                    "Nothing Left Out", systemImage: "cart.badge.minus",
                    description: Text(
                        "Swipe a line on the grocery list, or remove an ingredient from a recipe, to leave it out.")
                )
                .listRowBackground(Color.clear)
            }
            section(
                skips.forever, title: String(localized: "Never Buying"),
                footer: String(localized: "Left off every list until you buy it again."))
            section(
                skips.thisWeek, title: String(localized: "Just This Week"),
                footer: String(localized: "Back on the list next week, with nothing to do."))
            section(
                skips.inRecipes, title: String(localized: "Left Out of One Dish"),
                footer: String(localized: "Left out of that recipe every time it's planned. Other dishes still get it.")
            )
        }
        .refreshable {
            await skips.refresh()
        }
    }

    @ViewBuilder
    private func section(_ items: [GrocerySkip], title: String, footer: String) -> some View {
        if !items.isEmpty {
            Section {
                ForEach(items) { skip in
                    row(skip)
                }
            } header: {
                Text(title)
            } footer: {
                Text(footer)
            }
        }
    }

    private func row(_ skip: GrocerySkip) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(skip.name)
                if skip.scope == .recipe, let recipe = skip.recipeName, !recipe.isEmpty {
                    Text("in \(recipe)")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
            .accessibilityElement(children: .combine)
            Spacer(minLength: 0)
            if skips.isBusy(skip) {
                ProgressView()
            } else if canEdit, skip.scope == .recipe {
                Button("Put Back") { resume(skip) }
                    .buttonStyle(.bordered)
                    .accessibilityLabel("Put \(skip.name) Back in \(skip.recipeName ?? "the Recipe")")
            } else if canEdit {
                Menu {
                    Button("Buy It Again", systemImage: "arrow.uturn.backward") {
                        resume(skip)
                    }
                    if skip.isForever {
                        Button("Only Skip This Week", systemImage: "calendar") {
                            change(skip, to: .week)
                        }
                    } else {
                        Button("Never Buy It", systemImage: "nosign") {
                            change(skip, to: .always)
                        }
                    }
                } label: {
                    Image(systemName: "ellipsis.circle")
                        .foregroundStyle(.secondary)
                        .frame(minWidth: 44, minHeight: 44)
                }
                .accessibilityLabel("Change \(skip.name)")
            }
        }
        .swipeActions(edge: .trailing) {
            if canEdit {
                Button(
                    skip.scope == .recipe ? "Put Back" : "Buy It Again", systemImage: "arrow.uturn.backward"
                ) {
                    resume(skip)
                }
                .tint(.blue)
            }
        }
    }

    private func resume(_ skip: GrocerySkip) {
        run { try await skips.resume(skipID: skip.id) }
    }

    private func change(_ skip: GrocerySkip, to scope: GrocerySkipScope) {
        run { try await skips.skip(GrocerySkipRequest.changing(skip, to: scope, week: week)) }
    }

    private func run(_ change: @escaping () async throws -> Void) {
        Task {
            do {
                try await change()
            } catch is CancellationError {
                return
            } catch {
                actionError = HouseholdStore.message(for: error)
            }
        }
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        SkippedIngredientsView(week: .current(in: .gmt, weekStartsOn: PlanDay.defaultWeekStart))
    }
    .environment(HouseholdPreviewData.store(session: session))
    .environment(
        GrocerySkipStore.preview(
            session: session,
            items: [
                GrocerySkip(
                    id: "skip-1", ingredientKey: "name:cilantro", key: "cilantro", name: "Cilantro",
                    scope: .always, week: nil, text: "Never buying this"),
                GrocerySkip(
                    id: "skip-2", ingredientKey: "name:dill", key: "dill", name: "Dill",
                    scope: .week, week: "2026-W38", text: "Skipped this week"),
                GrocerySkip(
                    id: "skip-3", ingredientKey: "name:peanuts", key: "peanuts", name: "Peanuts",
                    scope: .recipe, week: nil, text: "Left out of Thai Coconut Curry Chicken",
                    recipeID: "recipe-1", recipeName: "Thai Coconut Curry Chicken"),
            ]))
}
